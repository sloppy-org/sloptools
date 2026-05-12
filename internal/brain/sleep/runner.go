package sleep

import (
	"context"
	"fmt"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/audit"
	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/cleanup"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// JudgeOpts is the input to RunJudge. The caller supplies the rendered
// sleep packet, the report path that will receive the cleaned final
// output, and the routing / ledger handles. The runner owns the bulk →
// classifier → escalate decision; callers do not need to know which
// tier ran.
type JudgeOpts struct {
	Packet           string
	ReportPath       string
	SystemPromptPath string
	BrainRoot        string
	RunID            string
	Sphere           string
	Stage            string
	AllowEdits       bool
	Router           *routing.Router
	Ledger           *ledger.Ledger
	Now              time.Time
}

// JudgeResult is the runner's output. Body is the final cleaned
// Markdown that should be persisted as the canonical sleep report.
// Escalated is true when paid tier ran (either via the pre-flight gate
// or after the classifier flagged the bulk output). EscalationReason is
// the deterministic classifier reason string.
type JudgeResult struct {
	Body              string
	Escalated         bool
	EscalationReason  string
	BulkSkipped       bool
	EscalationBackend string
	EscalationModel   string
}

// RunJudge runs the sleep-judge editorial pass with bulk → classifier
// → paid-escalation routing.
//
// Pipeline:
//
//  1. Pre-flight: if len(packet) > PreflightPacketCap (#129) the bulk
//     pass is skipped entirely and the packet is sent to the paid tier
//     directly. The 167 KB qwen context-window collapse that motivated
//     this gate is non-recoverable — the bulk model returns trigram
//     spam, not a report.
//  2. Bulk pass: local llamacpp qwen with the sleep-judge system prompt and
//     the packet on stdin. Output goes through cleanup.CleanReport.
//  3. Classifier: classifySleepJudgeOutput inspects the cleaned body
//     and returns a Decision. Signals: parse-error wrapper, leaked
//     <think>, non-printable ratio over 5%, or any 3-gram repeating
//     more than 30 times.
//  4. Paid escalation: when the classifier flags or the pre-flight
//     gate fires, route through routing.StageSleepJudge (codex/gpt-
//     5.4-mini) with the same packet on stdin. The paid output
//     replaces the bulk body.
//
// Audit sidecars are written next to the canonical report path under
// the same .bulk.md / .escalate.<backend>.md / .audit.json convention
// the scout pipeline uses.
func RunJudge(ctx context.Context, opts JudgeOpts) (*JudgeResult, error) {
	if opts.Router == nil {
		return nil, fmt.Errorf("sleep RunJudge: Router required")
	}
	if opts.Now.IsZero() {
		opts.Now = time.Now().UTC()
	}
	if opts.Stage == "" {
		opts.Stage = "sleep-judge-" + opts.Now.UTC().Format("150405")
	}
	startedAt := opts.Now
	stages := []audit.StageRecord{}
	preflight := classifySleepJudgeOutput("", len(opts.Packet))
	var (
		body       string
		bulkRec    *audit.StageRecord
		bulkReason string
	)
	if !preflight.Escalate {
		rec, cleaned, err := runBulk(ctx, opts)
		if err != nil {
			return nil, fmt.Errorf("sleep bulk: %w", err)
		}
		body = cleaned
		bulkRec = rec
		d := classifySleepJudgeOutput(cleaned, len(opts.Packet))
		bulkRec.ReasonAfter = d.Reason
		stages = append(stages, *bulkRec)
		if !d.Escalate {
			_ = audit.WriteFile(opts.ReportPath, audit.File{
				Path: opts.ReportPath, ReportPath: opts.ReportPath,
				RunID: opts.RunID, Sphere: opts.Sphere,
				StartedAt: startedAt, EndedAt: time.Now().UTC(),
				FinalStage: bulkRec.Stage, Stages: stages,
			})
			return &JudgeResult{Body: body}, nil
		}
		bulkReason = d.Reason
	} else {
		bulkReason = preflight.Reason
	}
	escRec, escBody, err := runEscalate(ctx, opts, bulkReason)
	if err != nil {
		// Bulk body is the safest fallback; if even bulk did not run
		// (pre-flight gate), return an error so the caller knows the
		// stage produced nothing.
		if body == "" {
			return nil, fmt.Errorf("sleep escalate: %w", err)
		}
		return &JudgeResult{Body: body, BulkSkipped: preflight.Escalate, EscalationReason: "attempted: " + bulkReason + "; failed: " + err.Error()}, nil
	}
	escRec.TriggerReason = bulkReason
	stages = append(stages, *escRec)
	finalStage := escRec.Stage
	_ = audit.WriteFile(opts.ReportPath, audit.File{
		Path: opts.ReportPath, ReportPath: opts.ReportPath,
		RunID: opts.RunID, Sphere: opts.Sphere,
		StartedAt: startedAt, EndedAt: time.Now().UTC(),
		FinalStage: finalStage, Escalated: true, Stages: stages,
	})
	return &JudgeResult{
		Body:              escBody,
		Escalated:         true,
		EscalationReason:  bulkReason,
		BulkSkipped:       preflight.Escalate,
		EscalationBackend: escRec.Backend,
		EscalationModel:   escRec.Model,
	}, nil
}

// runBulk runs the bulk editorial pass and returns the StageRecord plus the
// cleaned body. Both autonomy modes use LlamacppBackend (direct HTTP to
// slopgate, no subprocess). Plan-only uses MoE with no tools; full-autonomy
// uses MoE with the curated sleep-judge allowlist so sloppy_brain
// action=note_write can edit vault files. Paid escalation via runEscalate
// goes through Router.Pick(StageSleepJudge) → codex.
func runBulk(ctx context.Context, opts JudgeOpts) (*audit.StageRecord, string, error) {
	var bulkPick routing.Choice
	var allowList []string
	if opts.AllowEdits {
		bulkPick = routing.LlamacppMoEBulk()
		allowList = opts.Router.MCPToolsFor(routing.StageSleepJudge)
	} else {
		bulkPick = routing.LlamacppMoEBulk()
		// plan-only: pure text, no tools
	}
	be := backend.LlamacppBackend{}
	stage := opts.Stage + "-bulk"
	sb, err := backend.NewSandbox(opts.RunID, stage, opts.SystemPromptPath, backend.DefaultMCPConfig())
	if err != nil {
		return nil, "", fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()
	req := backend.Request{
		Stage:            stage,
		Packet:           opts.Packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            bulkPick.Model,
		Reasoning:        bulkPick.Reasoning,
		AllowEdits:       opts.AllowEdits,
		MCPAllowList:     allowList,
		Affinity:         backend.AffinityForPick(opts.RunID, opts.Sphere, stage),
		Sandbox:          sb,
		WorkDir:          opts.BrainRoot,
	}
	startedAt := time.Now().UTC()
	resp, err := be.Run(ctx, req)
	if err != nil {
		return nil, "", err
	}
	body := cleanup.CleanReport(resp.Output)
	if body == "" {
		return nil, "", fmt.Errorf("empty bulk output")
	}
	rawPath, cleanedPath, _ := audit.WriteStageArtifact(opts.ReportPath, "bulk", resp.Output, body)
	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    opts.Sphere,
			Stage:     stage,
			Provider:  bulkPick.Provider,
			Backend:   bulkPick.BackendID,
			Model:     bulkPick.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"tier": "bulk"},
		})
	}
	rec := &audit.StageRecord{
		Stage: stage, Backend: bulkPick.BackendID, Provider: string(bulkPick.Provider),
		Model: bulkPick.Model, Tier: "bulk",
		StartedAt: startedAt, WallMS: resp.WallMS,
		TokensIn: resp.TokensIn, TokensOut: resp.TokensOut, CostHint: resp.CostHint,
		RawPath: rawPath, CleanedPath: cleanedPath,
		RawBytes: len(resp.Output), CleanedBytes: len(body),
	}
	return rec, body, nil
}

// runEscalate routes the same packet to the paid tier. The router pick
// uses StageSleepJudge so codex/gpt-5.4-mini@medium is the default.
func runEscalate(ctx context.Context, opts JudgeOpts, reason string) (*audit.StageRecord, string, error) {
	pick, err := opts.Router.Pick(routing.StageSleepJudge)
	if err != nil {
		return nil, "", fmt.Errorf("router pick: %w", err)
	}
	if pick.Provider == backend.ProviderLocal {
		return nil, "", fmt.Errorf("paid tiers saturated; bulk-only fallback not allowed when classifier flagged")
	}
	be, err := backendForID(pick.BackendID)
	if err != nil {
		return nil, "", err
	}
	stage := opts.Stage + "-escalate"
	sb, err := backend.NewSandbox(opts.RunID, stage, opts.SystemPromptPath, backend.DefaultMCPConfig())
	if err != nil {
		return nil, "", fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()
	req := backend.Request{
		Stage:            stage,
		Packet:           opts.Packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       opts.AllowEdits,
		Sandbox:          sb,
		WorkDir:          opts.BrainRoot,
	}
	startedAt := time.Now().UTC()
	resp, err := be.Run(ctx, req)
	if err != nil {
		return nil, "", err
	}
	body := cleanup.CleanReport(resp.Output)
	if body == "" {
		return nil, "", fmt.Errorf("empty escalate output")
	}
	// Caller persists the final report (writeSleepReport in
	// internal/brain/sleep.go); the runner only emits sidecars +
	// audit JSON. Avoids a double-write race when the integrity gate
	// commits the canonical report.
	rawPath, cleanedPath, _ := audit.WriteStageArtifact(opts.ReportPath, "escalate."+pick.BackendID, resp.Output, body)
	if opts.Ledger != nil {
		_ = opts.Ledger.Append(ledger.Entry{
			Sphere:    opts.Sphere,
			Stage:     stage,
			Provider:  pick.Provider,
			Backend:   pick.BackendID,
			Model:     pick.Model,
			TokensIn:  resp.TokensIn,
			TokensOut: resp.TokensOut,
			WallMS:    resp.WallMS,
			CostHint:  resp.CostHint,
			Extras:    map[string]string{"tier": string(pick.Tier), "escalation": "true", "escalation_reason": reason},
		})
	}
	rec := &audit.StageRecord{
		Stage: stage, Backend: pick.BackendID, Provider: string(pick.Provider),
		Model: pick.Model, Tier: string(pick.Tier),
		StartedAt: startedAt, WallMS: resp.WallMS,
		TokensIn: resp.TokensIn, TokensOut: resp.TokensOut, CostHint: resp.CostHint,
		RawPath: rawPath, CleanedPath: cleanedPath,
		RawBytes: len(resp.Output), CleanedBytes: len(body),
	}
	return rec, body, nil
}

func backendForID(id string) (backend.Backend, error) {
	switch id {
	case "claude":
		return backend.ClaudeBackend{}, nil
	case "codex":
		return backend.CodexBackend{}, nil
	case "llamacpp":
		return backend.LlamacppBackend{}, nil
	}
	return nil, fmt.Errorf("unknown backend id %q", id)
}
