package bench

import (
	"encoding/json"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain/backend"
)

// MatrixTSVPath returns the path of the matrix.tsv emitted by Render.
func MatrixTSVPath(outDir string) string { return filepath.Join(outDir, "matrix.tsv") }

// ReportMDPath returns the path of the report.md emitted by Render.
func ReportMDPath(outDir string) string { return filepath.Join(outDir, "report.md") }

// Render writes matrix.tsv and report.md from the bench result.
func Render(res *Result) error {
	if err := writeMatrixTSV(res); err != nil {
		return err
	}
	return writeReportMD(res)
}

func writeMatrixTSV(res *Result) error {
	path := MatrixTSVPath(res.OutDir)
	var b strings.Builder
	b.WriteString("task\tfixture\tmodel\tprovider\tdraw\tpasses\tscore\twall_ms\ttokens_in\ttokens_out\tskipped\tjudge_used\tjudge_passes\tjudge_score\tinvented_facts\trationale\n")
	for _, c := range res.Cells {
		fmt.Fprintf(&b, "%s\t%s\t%s\t%s\t%d\t%v\t%.3f\t%d\t%d\t%d\t%v\t%v\t%v\t%.3f\t%s\t%s\n",
			c.TaskID, c.FixtureID, c.Model.Label, c.Model.Provider,
			c.Draw, c.Passes, c.Score, c.WallMS, c.TokensIn, c.TokensOut, c.Skipped,
			c.JudgeUsed, c.JudgePasses, c.JudgeScore,
			oneLine(strings.Join(c.JudgeFacts, "; ")),
			oneLine(c.Rationale))
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func writeReportMD(res *Result) error {
	var b strings.Builder
	b.WriteString("# Brain bench v1 report\n\n")
	fmt.Fprintf(&b, "Started: `%s`\nEnded:   `%s`\nDuration: %s\n\n",
		res.Started.Format(time.RFC3339),
		res.Ended.Format(time.RFC3339),
		res.Ended.Sub(res.Started).Truncate(time.Second))

	tasks := groupByTask(res.Cells)
	keys := make([]string, 0, len(tasks))
	for k := range tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, taskID := range keys {
		fmt.Fprintf(&b, "## Task: %s\n\n", taskID)
		writeTaskSection(&b, tasks[taskID])
		b.WriteString("\n")
	}
	writeSummarySection(&b, res.Cells)
	return os.WriteFile(ReportMDPath(res.OutDir), []byte(b.String()), 0o644)
}

func writeTaskSection(b *strings.Builder, cells []Cell) {
	models := groupByModel(cells)
	labels := make([]string, 0, len(models))
	for k := range models {
		labels = append(labels, k)
	}
	sort.Strings(labels)
	hasDraws := anyDrawsAboveOne(cells)
	if hasDraws {
		b.WriteString("| model | provider | struct pass | judge pass | mean struct ± stdev | mean judge | invented facts | mean wall (s) |\n")
		b.WriteString("|-------|----------|-------------|-----------|---------------------|-----------|-----------------|---------------|\n")
	} else {
		b.WriteString("| model | provider | struct pass | judge pass | mean struct | mean judge | invented facts | mean wall (s) |\n")
		b.WriteString("|-------|----------|-------------|-----------|-------------|-----------|-----------------|---------------|\n")
	}
	for _, label := range labels {
		group := models[label]
		var (
			passes, judgeRuns, judgePasses, invented, skipped int
			scores                                            []float64
			judgeScoreSum                                     float64
			wallSum                                           int64
		)
		for _, c := range group {
			if c.Skipped {
				skipped++
				continue
			}
			if c.Passes {
				passes++
			}
			scores = append(scores, c.Score)
			wallSum += c.WallMS
			if c.JudgeUsed {
				judgeRuns++
				if c.JudgePasses {
					judgePasses++
				}
				judgeScoreSum += c.JudgeScore
				invented += len(c.JudgeFacts)
			}
		}
		runs := len(group) - skipped
		passRate := 0.0
		meanScore := 0.0
		stdev := 0.0
		meanWallS := 0.0
		if runs > 0 {
			passRate = float64(passes) / float64(runs)
			meanScore, stdev = meanStdev(scores)
			meanWallS = float64(wallSum) / float64(runs) / 1000.0
		}
		judgePassRate := 0.0
		meanJudge := 0.0
		if judgeRuns > 0 {
			judgePassRate = float64(judgePasses) / float64(judgeRuns)
			meanJudge = judgeScoreSum / float64(judgeRuns)
		}
		if hasDraws {
			fmt.Fprintf(b, "| %s | %s | %.0f%% | %.0f%% | %.2f ± %.2f | %.2f | %d | %.1f |\n",
				label, group[0].Model.Provider, passRate*100, judgePassRate*100,
				meanScore, stdev, meanJudge, invented, meanWallS)
		} else {
			fmt.Fprintf(b, "| %s | %s | %.0f%% | %.0f%% | %.2f | %.2f | %d | %.1f |\n",
				label, group[0].Model.Provider, passRate*100, judgePassRate*100,
				meanScore, meanJudge, invented, meanWallS)
		}
	}
}

func meanStdev(xs []float64) (float64, float64) {
	if len(xs) == 0 {
		return 0, 0
	}
	var sum float64
	for _, x := range xs {
		sum += x
	}
	mean := sum / float64(len(xs))
	if len(xs) < 2 {
		return mean, 0
	}
	var sq float64
	for _, x := range xs {
		d := x - mean
		sq += d * d
	}
	return mean, math.Sqrt(sq / float64(len(xs)-1))
}

func anyDrawsAboveOne(cells []Cell) bool {
	for _, c := range cells {
		if c.Draw > 1 {
			return true
		}
	}
	return false
}

func writeSummarySection(b *strings.Builder, cells []Cell) {
	b.WriteString("## Summary recommendations\n\n")
	tasks := groupByTask(cells)
	keys := make([]string, 0, len(tasks))
	for k := range tasks {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, taskID := range keys {
		writeRecommendation(b, taskID, tasks[taskID])
	}
}

type modelStat struct {
	label     string
	provider  backend.Provider
	passRate  float64
	meanScore float64
	meanWall  float64
}

func writeRecommendation(b *strings.Builder, taskID string, cells []Cell) {
	models := groupByModel(cells)
	stats := make([]modelStat, 0, len(models))
	for label, group := range models {
		var passes, runs int
		var scoreSum float64
		var wallSum int64
		for _, c := range group {
			if c.Skipped {
				continue
			}
			runs++
			if c.Passes {
				passes++
			}
			scoreSum += c.Score
			wallSum += c.WallMS
		}
		if runs == 0 {
			continue
		}
		stats = append(stats, modelStat{
			label:     label,
			provider:  group[0].Model.Provider,
			passRate:  float64(passes) / float64(runs),
			meanScore: scoreSum / float64(runs),
			meanWall:  float64(wallSum) / float64(runs) / 1000.0,
		})
	}
	if len(stats) == 0 {
		return
	}
	sort.SliceStable(stats, func(i, j int) bool {
		if stats[i].meanScore != stats[j].meanScore {
			return stats[i].meanScore > stats[j].meanScore
		}
		return stats[i].meanWall < stats[j].meanWall
	})
	fmt.Fprintf(b, "- **%s**: top by score = `%s` (%.2f, %.1fs); cheap default candidate = `%s`.\n",
		taskID, stats[0].label, stats[0].meanScore, stats[0].meanWall, cheapDefaultLabel(stats))
}

func cheapDefaultLabel(stats []modelStat) string {
	for _, s := range stats {
		if s.provider == backend.ProviderLocal && s.passRate >= 0.7 {
			return s.label
		}
	}
	return stats[0].label
}

func groupByTask(cells []Cell) map[string][]Cell {
	out := map[string][]Cell{}
	for _, c := range cells {
		out[c.TaskID] = append(out[c.TaskID], c)
	}
	return out
}

func groupByModel(cells []Cell) map[string][]Cell {
	out := map[string][]Cell{}
	for _, c := range cells {
		out[c.Model.Label] = append(out[c.Model.Label], c)
	}
	return out
}

func oneLine(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\t", " "), "\n", " ")
}

func saveRaw(outDir string, cell Cell) error {
	suffix := ""
	if cell.Draw > 1 {
		suffix = fmt.Sprintf("-d%d", cell.Draw)
	}
	path := filepath.Join(outDir, "raw", fmt.Sprintf("%s-%s-%s%s.json", cell.TaskID, cell.FixtureID, sanitize(cell.Model.Label), suffix))
	body, err := json.MarshalIndent(cell, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, body, 0o644)
}
