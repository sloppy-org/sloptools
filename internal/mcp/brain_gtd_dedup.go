package mcp

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

// WriteQuickMeetingCommitment renders and writes an inbox-status GTD
// commitment for a short voice memo. The commitment carries
// `provider: meetings`, `status: inbox`, the recognised outcome as both
// title and `## Next Action`, and the verbatim transcript under
// `## Evidence`. The written file's ref is `<slug>#<id>` where `id` is a
// stable hash of the audio basename plus transcript so re-running the
// pipeline against the same audio is idempotent. Co-located with the
// dedup helpers so brain_gtd_ingest_meetings.go stays under the 500-line
// per-file limit while the mcp package sits at its 50-file cap.
func WriteQuickMeetingCommitment(brainConfigPath, sphere, outcome, transcript, audioPath string) (string, error) {
	if strings.TrimSpace(sphere) == "" {
		return "", errors.New("sphere is required")
	}
	if strings.TrimSpace(outcome) == "" {
		return "", errors.New("outcome is required")
	}
	if strings.TrimSpace(transcript) == "" {
		return "", errors.New("transcript is required")
	}
	if strings.TrimSpace(audioPath) == "" {
		return "", errors.New("audio path is required")
	}
	cfg, err := brain.LoadConfig(brainConfigPath)
	if err != nil {
		return "", err
	}
	base := filepath.Base(audioPath)
	slug := slugifyAudioBase(base)
	id := quickMemoID(base, transcript)
	out := filepath.ToSlash(filepath.Join("brain", "gtd", "ingest", slug+"-"+id+".md"))
	target, err := brain.ResolveNotePath(cfg, brain.Sphere(sphere), out)
	if err != nil {
		return "", err
	}
	if err := os.MkdirAll(filepath.Dir(target.Path), 0o755); err != nil {
		return "", err
	}
	binding := braingtd.SourceBinding{
		Provider:  meetingsProvider,
		Ref:       slug + "#" + id,
		Location:  braingtd.BindingLocation{Path: filepath.ToSlash(audioPath)},
		Writeable: true,
	}
	rendered := renderQuickMeetingCommitment(sphere, outcome, transcript, base, binding)
	if err := validateRenderedBrainGTD(rendered); err != nil {
		return "", err
	}
	if err := os.WriteFile(target.Path, []byte(rendered), 0o644); err != nil {
		return "", err
	}
	return target.Rel, nil
}

func renderQuickMeetingCommitment(sphere, outcome, transcript, audioBase string, binding braingtd.SourceBinding) string {
	locationLine := ""
	if locationPath := strings.TrimSpace(binding.Location.Path); locationPath != "" {
		locationLine = "    location:\n      path: " + locationPath + "\n"
	}
	body := fmt.Sprintf(`---
kind: commitment
sphere: %s
title: %q
status: inbox
context: meetings
source_bindings:
  - provider: %s
    ref: %q
    writeable: true
%s---
# %s

## Summary
Quick voice memo: %s.

## Next Action
- [ ] %s

## Evidence
- audio: %s
- transcript: %s

## Linked Items
- None.

## Review Notes
- Captured from voice memo %s.
`,
		sphere, outcome, binding.Provider, binding.Ref, locationLine,
		outcome, audioBase, outcome, audioBase,
		transcriptForEvidence(transcript), audioBase,
	)
	return strings.TrimSpace(body) + "\n"
}

func transcriptForEvidence(transcript string) string {
	collapsed := strings.Join(strings.Fields(transcript), " ")
	if len(collapsed) > 600 {
		return collapsed[:600] + "..."
	}
	return collapsed
}

func slugifyAudioBase(name string) string {
	stem := strings.TrimSuffix(name, filepath.Ext(name))
	cleaned := slugify(stem)
	if cleaned == "" {
		return "voice-memo"
	}
	return cleaned
}

func quickMemoID(audioBase, transcript string) string {
	sum := sha1.Sum([]byte(audioBase + "\x00" + transcript))
	return hex.EncodeToString(sum[:])[:meetings.IDLength]
}

type dedupNote struct {
	Entry    braingtd.CommitmentEntry
	Note     *brain.MarkdownNote
	Resolved brain.ResolvedPath
}

func (s *Server) brainGTDDedupScan(args map[string]interface{}) (map[string]interface{}, error) {
	notes, opts, err := s.loadDedupNotes(args)
	if err != nil {
		return nil, err
	}
	result := braingtd.Scan(dedupEntries(notes), opts)
	return map[string]interface{}{"sphere": strArg(args, "sphere"), "dedup": result}, nil
}

func (s *Server) brainGTDDedupReviewApply(args map[string]interface{}) (map[string]interface{}, error) {
	notes, opts, err := s.loadDedupNotes(args)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(strArg(args, "id"))
	decision := strings.TrimSpace(strArg(args, "decision"))
	if id == "" || decision == "" {
		return nil, errors.New("id and decision are required")
	}
	pair, err := candidatePair(notes, opts, id)
	if err != nil {
		return nil, err
	}
	switch decision {
	case "merge":
		return applyDedupMerge(pair, args, id)
	case "keep_separate":
		braingtd.MarkNotDuplicate(&pair[0].Entry, &pair[1].Entry, id)
	case "defer":
		braingtd.MarkDeferred(&pair[0].Entry, &pair[1].Entry, id)
	default:
		return nil, fmt.Errorf("unsupported dedup decision %q", decision)
	}
	if err := writeDedupNotes(pair[:]...); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "decision": decision, "paths": []string{pair[0].Entry.Path, pair[1].Entry.Path}}, nil
}

func (s *Server) brainGTDDedupHistory(args map[string]interface{}) (map[string]interface{}, error) {
	notes, _, err := s.loadDedupNotes(args)
	if err != nil {
		return nil, err
	}
	var history []map[string]interface{}
	for _, note := range notes {
		dedup := note.Entry.Commitment.Dedup
		if dedup.Empty() {
			continue
		}
		history = append(history, map[string]interface{}{"path": note.Entry.Path, "dedup": dedup})
	}
	return map[string]interface{}{"sphere": strArg(args, "sphere"), "history": history, "count": len(history)}, nil
}

func (s *Server) brainGTDBind(args map[string]interface{}) (map[string]interface{}, error) {
	notes, _, err := s.loadDedupNotes(args)
	if err != nil {
		return nil, err
	}
	paths := stringListArg(args, "paths")
	winnerPath := strings.TrimSpace(strArg(args, "winner_path"))
	if winnerPath == "" {
		winnerPath = strings.TrimSpace(strArg(args, "path"))
	}
	if winnerPath == "" && len(paths) > 0 {
		winnerPath = paths[0]
	}
	if winnerPath == "" {
		return nil, errors.New("winner_path or path is required")
	}
	if !containsPath(paths, winnerPath) {
		paths = append([]string{winnerPath}, paths...)
	}
	byPath := dedupNotesByPath(notes)
	winner, ok := byPath[winnerPath]
	if !ok {
		return nil, fmt.Errorf("winner_path %q not found", winnerPath)
	}
	outcome := strings.TrimSpace(strArg(args, "outcome"))
	if outcome == "" {
		outcome = winner.Entry.Commitment.Outcome
	}
	extraBindings, err := sourceBindingsArg(args, "source_bindings")
	if err != nil {
		return nil, err
	}
	changed := map[string]dedupNote{winner.Entry.Path: winner}
	var merged []string
	for _, path := range paths {
		note, ok := byPath[path]
		if !ok {
			return nil, fmt.Errorf("path %q not found", path)
		}
		if note.Entry.Path == winner.Entry.Path {
			continue
		}
		if !sameBindOutcome(winner.Entry.Commitment, note.Entry.Commitment) {
			return nil, fmt.Errorf("path %q does not match winner outcome", path)
		}
		braingtd.ApplyMerge(&winner.Entry, &note.Entry, braingtd.CandidateID(winner.Entry.Path, note.Entry.Path), outcome, "")
		changed[note.Entry.Path] = note
		merged = append(merged, note.Entry.Path)
	}
	winner.Entry.Commitment.SourceBindings = braingtd.MergeSourceBindings(winner.Entry.Commitment.SourceBindings, extraBindings)
	if strings.TrimSpace(outcome) != "" {
		winner.Entry.Commitment.Outcome = strings.TrimSpace(outcome)
	}
	changed[winner.Entry.Path] = winner
	if err := writeDedupNotes(mapValues(changed)...); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere":        strArg(args, "sphere"),
		"winner_path":   winner.Entry.Path,
		"merged_paths":  merged,
		"binding_count": len(winner.Entry.Commitment.SourceBindings),
	}, nil
}

func (s *Server) loadDedupNotes(args map[string]interface{}) ([]dedupNote, braingtd.ScanOptions, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, braingtd.ScanOptions{}, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, braingtd.ScanOptions{}, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, braingtd.ScanOptions{}, fmt.Errorf("unknown vault %q", sphere)
	}
	opts, err := dedupScanOptions(args)
	if err != nil {
		return nil, braingtd.ScanOptions{}, err
	}
	notes, err := readDedupNotes(vault)
	return notes, opts, err
}

func dedupScanOptions(args map[string]interface{}) (braingtd.ScanOptions, error) {
	cfg, err := braingtd.LoadDedupConfig(strArg(args, "dedup_config"))
	if err != nil {
		return braingtd.ScanOptions{}, err
	}
	opts := cfg.ScanOptions()
	if v := floatArg(args, "deterministic_threshold"); v > 0 {
		opts.DeterministicThreshold = v
	}
	if v := floatArg(args, "llm_threshold"); v > 0 {
		opts.LLMThreshold = v
	}
	if v := floatArg(args, "candidate_threshold"); v > 0 {
		opts.CandidateThreshold = v
	}
	if cmd := strings.TrimSpace(strArg(args, "llm_command")); cmd != "" {
		opts.LLM = braingtd.CommandReviewer{Command: cmd, Timeout: 15 * time.Second}
	}
	return opts, nil
}

func readDedupNotes(vault brain.Vault) ([]dedupNote, error) {
	var notes []dedupNote
	err := filepath.WalkDir(vault.BrainRoot(), func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		kind, note, diags := brainNoteKind(string(data))
		if len(diags) != 0 || kind != "commitment" {
			return nil
		}
		commitment, note, diags := braingtd.ParseCommitmentMarkdown(string(data))
		if len(diags) != 0 {
			return nil
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		resolved := brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: vault.BrainRoot(), Path: path, Rel: filepath.ToSlash(rel)}
		entry := braingtd.CommitmentEntry{Path: resolved.Rel, Commitment: *commitment}
		notes = append(notes, dedupNote{Entry: entry, Note: note, Resolved: resolved})
		return nil
	})
	return notes, err
}

func dedupNotesByPath(notes []dedupNote) map[string]dedupNote {
	out := make(map[string]dedupNote, len(notes))
	for _, note := range notes {
		out[note.Entry.Path] = note
	}
	return out
}

func dedupEntries(notes []dedupNote) []braingtd.CommitmentEntry {
	entries := make([]braingtd.CommitmentEntry, 0, len(notes))
	for _, note := range notes {
		entries = append(entries, note.Entry)
	}
	return entries
}

func candidatePair(notes []dedupNote, opts braingtd.ScanOptions, id string) ([2]dedupNote, error) {
	result := braingtd.Scan(dedupEntries(notes), opts)
	byPath := dedupNotesByPath(notes)
	for _, candidate := range result.Candidates {
		if candidate.ID == id && len(candidate.Paths) == 2 {
			return [2]dedupNote{byPath[candidate.Paths[0]], byPath[candidate.Paths[1]]}, nil
		}
	}
	return [2]dedupNote{}, fmt.Errorf("dedup candidate %q not found", id)
}

func sameBindOutcome(a, b braingtd.Commitment) bool {
	return bindOutcome(a) != "" && bindOutcome(a) == bindOutcome(b)
}

func bindOutcome(commitment braingtd.Commitment) string {
	value := strings.TrimSpace(commitment.Outcome)
	if value == "" {
		value = commitment.Title
	}
	return strings.ToLower(strings.Join(strings.Fields(value), " "))
}

func sourceBindingsArg(args map[string]interface{}, key string) ([]braingtd.SourceBinding, error) {
	raw, ok := args[key]
	if !ok {
		return nil, nil
	}
	items, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be an array", key)
	}
	bindings := make([]braingtd.SourceBinding, 0, len(items))
	for _, item := range items {
		fields, ok := item.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s entries must be objects", key)
		}
		binding := braingtd.SourceBinding{
			Provider: strArg(fields, "provider"),
			Ref:      strArg(fields, "ref"),
			URL:      strArg(fields, "url"),
			Summary:  strArg(fields, "summary"),
		}
		if binding.StableID() == "" {
			return nil, fmt.Errorf("%s entries require provider and ref", key)
		}
		bindings = append(bindings, binding)
	}
	return bindings, nil
}

func containsPath(paths []string, want string) bool {
	for _, path := range paths {
		if path == want {
			return true
		}
	}
	return false
}

func mapValues(values map[string]dedupNote) []dedupNote {
	out := make([]dedupNote, 0, len(values))
	for _, value := range values {
		out = append(out, value)
	}
	return out
}

func applyDedupMerge(pair [2]dedupNote, args map[string]interface{}, id string) (map[string]interface{}, error) {
	winnerPath := strings.TrimSpace(strArg(args, "winner_path"))
	if winnerPath == "" {
		winnerPath = pair[0].Entry.Path
	}
	winner, loser, err := orderedMergePair(pair, winnerPath)
	if err != nil {
		return nil, err
	}
	braingtd.ApplyMerge(&winner.Entry, &loser.Entry, id, strArg(args, "outcome"), "")
	if err := writeDedupNotes(*winner, *loser); err != nil {
		return nil, err
	}
	return map[string]interface{}{"id": id, "decision": "merge", "winner_path": winner.Entry.Path, "merged_path": loser.Entry.Path}, nil
}

func orderedMergePair(pair [2]dedupNote, winnerPath string) (*dedupNote, *dedupNote, error) {
	if pair[0].Entry.Path == winnerPath {
		return &pair[0], &pair[1], nil
	}
	if pair[1].Entry.Path == winnerPath {
		return &pair[1], &pair[0], nil
	}
	return nil, nil, fmt.Errorf("winner_path %q is not in candidate", winnerPath)
}

func writeDedupNotes(notes ...dedupNote) error {
	for _, note := range notes {
		if strings.TrimSpace(note.Entry.Commitment.Outcome) != "" {
			if err := note.Note.SetFrontMatterField("outcome", note.Entry.Commitment.Outcome); err != nil {
				return err
			}
		}
		if err := braingtd.ApplyCommitment(note.Note, note.Entry.Commitment); err != nil {
			return err
		}
		rendered, err := note.Note.Render()
		if err != nil {
			return err
		}
		if err := validateRenderedBrainGTD(rendered); err != nil {
			return err
		}
		if err := os.WriteFile(note.Resolved.Path, []byte(rendered), 0o644); err != nil {
			return err
		}
	}
	return nil
}

func floatArg(args map[string]interface{}, key string) float64 {
	switch v := args[key].(type) {
	case float64:
		return v
	case int:
		return float64(v)
	case int64:
		return float64(v)
	default:
		return 0
	}
}
