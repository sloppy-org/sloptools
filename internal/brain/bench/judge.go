package bench

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
	"github.com/sloppy-org/sloptools/internal/brain/prompts"
)

// Judge runs a paid model over a (fixture, candidate-output) pair to
// catch quality issues the deterministic rubric misses: invented
// domain facts, dropped wikilinks, degenerate H1s, empty Open Questions
// when the input has clear questions to ask. The judge writes a JSON
// verdict that the report renders alongside the deterministic score.
//
// Use a model the bench has already shown to be hallucination-free on
// this task.
type Judge struct {
	BackendID string
	Model     string
	Reasoning backend.Reasoning
}

// Verdict is the parsed JSON output of the bench-judge prompt.
type Verdict struct {
	Passes        bool     `json:"passes"`
	Score         float64  `json:"score"`
	Rationale     string   `json:"rationale"`
	InventedFacts []string `json:"invented_facts,omitempty"`
}

// runJudge invokes the configured judge backend on one cell. Errors
// surface to the caller; cell.JudgeErr captures them so the bench does
// not abort on a single judge failure.
func runJudge(ctx context.Context, j Judge, runID string, taskID string, f Fixture, out string) (Verdict, error) {
	rubric := taskRubric(taskID)
	packet := buildJudgePacket(f, out, rubric)
	stagePrompt, err := prompts.Read("bench-judge")
	if err != nil {
		return Verdict{}, err
	}
	tmp := filepath.Join(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Clean(filepath.Join("/tmp", runID))))), runID+"-judge-"+sanitize(f.ID))
	_ = tmp
	stagePromptPath, err := writeJudgePromptFile(runID, taskID, f.ID, stagePrompt)
	if err != nil {
		return Verdict{}, err
	}
	sb, err := backend.NewSandbox(runID, "judge-"+taskID+"-"+f.ID, stagePromptPath, backend.DefaultMCPConfig())
	if err != nil {
		return Verdict{}, err
	}
	defer sb.Cleanup()
	be, err := newBackendByID(j.BackendID)
	if err != nil {
		return Verdict{}, err
	}
	resp, err := be.Run(ctx, backend.Request{
		Stage:            "bench-judge",
		Packet:           packet,
		SystemPromptPath: sb.SystemPromptIn,
		Model:            j.Model,
		Reasoning:        j.Reasoning,
		AllowEdits:       false,
		Sandbox:          sb,
	})
	if err != nil {
		return Verdict{}, err
	}
	v := parseVerdict(resp.Output)
	return v, nil
}

func writeJudgePromptFile(runID, taskID, fixtureID string, body []byte) (string, error) {
	dir := filepath.Join("/tmp", "sloptools-bench-"+runID+"-judge")
	if err := mkdirAll(dir); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "bench-judge-"+taskID+"-"+fixtureID+".md")
	return path, writeFile(path, body)
}

func taskRubric(taskID string) string {
	switch taskID {
	case "folder-note":
		return strings.Join([]string{
			"Grade a strict folder-note Markdown body against the input packet.",
			"Penalise any of:",
			"  - any fact in the body that is not derivable from the input packet (invented facts, especially fabricated domain claims like calling a code that handles 1/nu transport a 'neutron transport' code)",
			"  - degenerate H1 (e.g. just a slugged folder name like 'wsd' instead of a course title)",
			"  - empty `## Notes` or `## Open Questions` when the packet supplies content for them",
			"  - wikilinks that point at paths not present in the packet's listed files / folders / related notes",
			"  - prose that reads like Wikipedia background rather than packet-derived content",
			"Score is in [0, 1]. passes = true iff zero invented_facts and no degenerate sections.",
		}, "\n")
	}
	return "Grade the candidate Markdown against the input packet. Penalise invented facts and degenerate sections."
}

func buildJudgePacket(f Fixture, out, rubric string) string {
	var b strings.Builder
	fmt.Fprintln(&b, "# Bench judge packet")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Rubric")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, rubric)
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Original input packet handed to the candidate")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b, f.Packet)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "## Candidate Markdown body")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b, out)
	fmt.Fprintln(&b, "```")
	fmt.Fprintln(&b)
	fmt.Fprintln(&b, "Return one JSON object with keys passes, score, rationale, invented_facts. No commentary.")
	return b.String()
}

func parseVerdict(raw string) Verdict {
	raw = strings.TrimSpace(raw)
	raw = stripCodeFenceJSON(raw)
	var v Verdict
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		// Fallback: report unparseable as zero-score with the raw output.
		return Verdict{Passes: false, Score: 0, Rationale: "judge-unparseable: " + truncate(raw, 200)}
	}
	return v
}

func stripCodeFenceJSON(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```json") {
		s = strings.TrimPrefix(s, "```json")
	} else if strings.HasPrefix(s, "```") {
		s = strings.TrimPrefix(s, "```")
	}
	s = strings.TrimSuffix(s, "```")
	return strings.TrimSpace(s)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}
