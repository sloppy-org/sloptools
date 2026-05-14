package scout

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/ledger"
	"github.com/sloppy-org/sloptools/internal/brain/prompts"
	"github.com/sloppy-org/sloptools/internal/brain/routing"
)

// selfResolveOne runs a free self-resolve pass over a bulk-tier report
// that the classifier flagged. resolvePick is the routing decision for the
// resolve pass. The agent reads its own prior draft plus the original packet
// and produces a refined report that either resolves flagged items with
// citations or marks them `- needs paid review:` for the escalation pass.
// The new body overwrites the report file; ledger gets a second entry tagged
// with the self-resolve stage. Returns the new body so the caller can
// re-classify it. Non-fatal errors are returned so the caller can decide
// whether to fall through to paid escalation.
func selfResolveOne(ctx context.Context, opts RunOpts, p Pick, originalPacket, bulkReport, reason, reportPath string, passNum int, resolvePick routing.Pick, broken *backend.BrokenTools) (string, stageRecord, error) {
	var rec stageRecord
	pick := resolvePick
	be := backendForPick(pick)
	stagePrompt, err := writeSelfResolvePrompt()
	if err != nil {
		return "", rec, fmt.Errorf("write self-resolve prompt: %w", err)
	}
	defer os.Remove(stagePrompt)
	stage := "scout-resolve-" + sanitize(p.Path)
	sb, err := backend.NewSandbox(opts.RunID, stage, stagePrompt, backend.DefaultMCPConfig())
	if err != nil {
		return "", rec, fmt.Errorf("sandbox: %w", err)
	}
	defer sb.Cleanup()
	packet := buildSelfResolvePacket(p, originalPacket, bulkReport, reason)
	startedAt := time.Now().UTC()
	resp, err := be.Run(ctx, backend.Request{
		Stage:            stage,
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            pick.Model,
		Reasoning:        pick.Reasoning,
		AllowEdits:       false,
		MCPAllowList:     pick.MCPTools,
		MCPToolQuotas:    pick.MCPQuotas,
		MCPBrokenTools:   broken,
		Affinity:         backend.AffinityForPick(opts.RunID, p.Path, "scout-resolve"),
		Sandbox:          sb,
	})
	if err != nil {
		return "", rec, fmt.Errorf("backend run: %w", err)
	}
	body := cleanReport(resp.Output)
	if body == "" {
		return "", rec, fmt.Errorf("empty self-resolve output")
	}
	if err := os.WriteFile(reportPath, []byte(body+"\n"), 0o644); err != nil {
		return "", rec, fmt.Errorf("write self-resolved report: %w", err)
	}
	rawPath, cleanedPath, _ := writeStageArtifact(reportPath, fmt.Sprintf("resolve.%d", passNum), resp.Output, body)
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
			Extras:    map[string]string{"path": p.Path, "tier": string(pick.Tier), "self_resolve": "true", "resolve_reason": reason},
		})
	}
	rec = stageRecord{
		Stage:        stage,
		Backend:      pick.BackendID,
		Provider:     string(pick.Provider),
		Model:        pick.Model,
		Tier:         string(pick.Tier),
		StartedAt:    startedAt,
		WallMS:       resp.WallMS,
		TokensIn:     resp.TokensIn,
		TokensOut:    resp.TokensOut,
		CostHint:     resp.CostHint,
		RawPath:      rawPath,
		CleanedPath:  cleanedPath,
		RawBytes:     len(resp.Output),
		CleanedBytes: len(body),
	}
	return body, rec, nil
}

// writeSelfResolvePrompt drops the close-the-gaps prompt to disk for
// the self-resolve call. The prompt is in internal/brain/prompts/
// scout-resolve.md and is extracted into a temp file so the sandbox
// can copy it (the sandbox needs a real file path).
func writeSelfResolvePrompt() (string, error) {
	dir, err := os.MkdirTemp("", "sloptools-scout-resolve-prompt-")
	if err != nil {
		return "", err
	}
	if _, err := prompts.Extract(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "scout-resolve.md")
	if _, err := os.Stat(path); err != nil {
		// Older prompt sets may not have the file; fall back to scout.md
		// so the call still runs (with the broader exploratory prompt).
		path = filepath.Join(dir, "scout.md")
	}
	return path, nil
}

// buildSelfResolvePacket bundles the original packet, the bulk report,
// and the conflict reason for the same opencode model on its second
// pass.
func buildSelfResolvePacket(p Pick, originalPacket, bulkReport, reason string) string {
	var b strings.Builder
	b.WriteString("# Scout self-resolve packet\n\n")
	fmt.Fprintf(&b, "Path: `%s`\n", p.Path)
	fmt.Fprintf(&b, "Title: %s\n", p.Title)
	fmt.Fprintf(&b, "Classifier flagged the prior draft because: %s\n\n", reason)
	b.WriteString("## Original entity packet\n\n")
	b.WriteString(originalPacket)
	b.WriteString("\n\n## Your prior draft scout report\n\n")
	b.WriteString(bulkReport)
	b.WriteString("\n\n## Your task\n\n")
	b.WriteString("Resolve each flagged item with a targeted MCP query, or mark genuinely-unresolvable items with `- needs paid review:` so the next pass can route them. Rewrite the entire scout report in the same section structure.\n")
	return b.String()
}
