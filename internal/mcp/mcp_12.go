package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
	"github.com/sloppy-org/sloptools/internal/evernote"
	"github.com/sloppy-org/sloptools/internal/store"
)

func isEvernoteProvider(provider string) bool {
	return strings.EqualFold(strings.TrimSpace(provider), store.ExternalProviderEvernote)
}

func (s *Server) evernoteClientForTool(args map[string]interface{}) (store.ExternalAccount, *evernote.Client, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := accountForTool(st, args, "evernote-capable", isEvernoteProvider)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	token, _, err := st.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	client, err := evernote.NewClient(token)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, client, nil
}

func (s *Server) dispatchEvernote(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "evernote_notebook_list":
		return s.evernoteNotebookList(args)
	case "evernote_note_search":
		return s.evernoteNoteSearch(args)
	case "evernote_note_get":
		return s.evernoteNoteGet(args)
	default:
		return nil, fmt.Errorf("unknown evernote method: %s", method)
	}
}

func (s *Server) evernoteNotebookList(args map[string]interface{}) (map[string]interface{}, error) {
	account, client, err := s.evernoteClientForTool(args)
	if err != nil {
		return nil, err
	}
	notebooks, err := client.ListNotebooks(context.Background())
	if err != nil {
		return nil, err
	}
	payloads := make([]map[string]interface{}, 0, len(notebooks))
	for _, notebook := range notebooks {
		payloads = append(payloads, map[string]interface{}{
			"id":         notebook.ID,
			"name":       notebook.Name,
			"stack":      notebook.Stack,
			"updated_at": notebook.UpdatedAt,
		})
	}
	return map[string]interface{}{"account_id": account.ID, "provider": account.Provider, "notebooks": payloads, "count": len(payloads)}, nil
}

func (s *Server) evernoteNoteSearch(args map[string]interface{}) (map[string]interface{}, error) {
	account, client, err := s.evernoteClientForTool(args)
	if err != nil {
		return nil, err
	}
	limit := intArg(args, "limit", 20)
	if limit <= 0 || limit > 50 {
		limit = 20
	}
	notes, err := client.ListNotes(context.Background(), strArg(args, "notebook_id"), evernote.ListNotesOptions{
		Query:        strArg(args, "query"),
		Tag:          strArg(args, "tag"),
		UpdatedAfter: strArg(args, "updated_after"),
		Limit:        limit,
	})
	if err != nil {
		return nil, err
	}
	payloads := make([]map[string]interface{}, 0, len(notes))
	for _, note := range notes {
		payloads = append(payloads, evernoteSummaryPayload(note))
	}
	return map[string]interface{}{"account_id": account.ID, "provider": account.Provider, "notes": payloads, "count": len(payloads)}, nil
}

func (s *Server) evernoteNoteGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, client, err := s.evernoteClientForTool(args)
	if err != nil {
		return nil, err
	}
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	note, err := client.GetNote(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": account.Provider, "note": evernoteNotePayload(note)}, nil
}

func evernoteSummaryPayload(note evernote.NoteSummary) map[string]interface{} {
	return map[string]interface{}{
		"id":           note.ID,
		"notebook_id":  note.NotebookID,
		"title":        note.Title,
		"created_at":   note.CreatedAt,
		"updated_at":   note.UpdatedAt,
		"tag_names":    append([]string(nil), note.TagNames...),
		"content_text": note.ContentText,
	}
}

func evernoteNotePayload(note evernote.Note) map[string]interface{} {
	tasks := make([]map[string]interface{}, 0, len(note.Tasks))
	for _, task := range note.Tasks {
		tasks = append(tasks, map[string]interface{}{"text": task.Text, "checked": task.Checked})
	}
	return map[string]interface{}{
		"id":               note.ID,
		"notebook_id":      note.NotebookID,
		"title":            note.Title,
		"created_at":       note.CreatedAt,
		"updated_at":       note.UpdatedAt,
		"tag_names":        append([]string(nil), note.TagNames...),
		"content_text":     note.ContentText,
		"content_markdown": note.ContentMarkdown,
		"tasks":            tasks,
	}
}

func (s *Server) brainGTDWrite(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	path := strings.TrimSpace(strArg(args, "path"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if path == "" {
		return nil, errors.New("path is required")
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(sphere), path)
	if err != nil {
		return nil, err
	}
	updates := objectArg(args, "commitment")
	if len(updates) == 0 {
		updates = noteWriteUpdates(args)
	}
	body, readErr := os.ReadFile(resolved.Path)
	updated := braingtd.Commitment{Kind: "commitment", Sphere: sphere}
	var note *brain.MarkdownNote
	var diags []brain.MarkdownDiagnostic
	if readErr == nil {
		commitment, parsedNote, parsedDiags := braingtd.ParseCommitmentMarkdown(string(body))
		if len(parsedDiags) != 0 {
			return map[string]interface{}{
				"source":      resolved,
				"diagnostics": parsedDiags,
				"count":       len(parsedDiags),
			}, nil
		}
		updated = *commitment
		note = parsedNote
		diags = parsedDiags
	} else if !errors.Is(readErr, os.ErrNotExist) {
		return nil, readErr
	}
	updated, err = overlayCommitment(updated, updates)
	if err != nil {
		return nil, err
	}
	if readErr == nil {
		if err := writeCommitmentFrontMatter(note, updated); err != nil {
			return nil, err
		}
		if err := braingtd.ApplyCommitment(note, updated); err != nil {
			return nil, err
		}
		rendered, err := note.Render()
		if err != nil {
			return nil, err
		}
		if err := validateRenderedBrainGTD(rendered); err != nil {
			return nil, err
		}
		if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
			return nil, err
		}
		return withAffected(
			map[string]interface{}{
				"source":      resolved,
				"commitment":  updated,
				"diagnostics": diags,
				"count":       len(diags),
				"valid":       len(diags) == 0,
			},
			brainCommitmentAffectedRef(sphere, resolved.Rel),
		), nil
	}
	rendered, err := braincatalog.BuildGTDCommitmentMarkdown(updated)
	if err != nil {
		return nil, err
	}
	if err := validateRenderedBrainGTD(rendered); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(filepath.Dir(resolved.Path), 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		return nil, err
	}
	return withAffected(
		map[string]interface{}{
			"source":      resolved,
			"commitment":  updated,
			"diagnostics": []brain.MarkdownDiagnostic{},
			"count":       0,
			"valid":       true,
		},
		brainCommitmentAffectedRef(sphere, resolved.Rel),
	), nil
}

func objectArg(args map[string]interface{}, key string) map[string]interface{} {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil
	}
	m, ok := raw.(map[string]interface{})
	if !ok {
		return nil
	}
	return m
}

func stringArgFromMap(m map[string]interface{}, key string) (string, bool) {
	raw, ok := m[key]
	if !ok {
		return "", false
	}
	value, ok := raw.(string)
	if !ok {
		return "", false
	}
	clean := strings.TrimSpace(value)
	return clean, true
}

func stringSliceArgFromMap(m map[string]interface{}, key string) ([]string, bool) {
	raw, ok := m[key]
	if !ok {
		return nil, false
	}
	switch typed := raw.(type) {
	case []string:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if clean := strings.TrimSpace(value); clean != "" {
				out = append(out, clean)
			}
		}
		return out, true
	case []interface{}:
		out := make([]string, 0, len(typed))
		for _, value := range typed {
			if clean := strings.TrimSpace(fmt.Sprint(value)); clean != "" && clean != "<nil>" {
				out = append(out, clean)
			}
		}
		return out, true
	case string:
		parts := strings.Split(typed, ",")
		out := make([]string, 0, len(parts))
		for _, value := range parts {
			if clean := strings.TrimSpace(value); clean != "" {
				out = append(out, clean)
			}
		}
		return out, true
	default:
		return nil, false
	}
}

func sourceBindingsFromAny(raw interface{}) ([]braingtd.SourceBinding, error) {
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var bindings []braingtd.SourceBinding
	if err := json.Unmarshal(body, &bindings); err != nil {
		return nil, err
	}
	for i := range bindings {
		bindings[i].Provider = strings.TrimSpace(bindings[i].Provider)
		bindings[i].Ref = strings.TrimSpace(bindings[i].Ref)
	}
	return bindings, nil
}

func decodeAny[T any](raw interface{}) (T, error) {
	var out T
	body, err := json.Marshal(raw)
	if err != nil {
		return out, err
	}
	if err := json.Unmarshal(body, &out); err != nil {
		return out, err
	}
	return out, nil
}

func writeCommitmentFrontMatter(note *brain.MarkdownNote, commitment braingtd.Commitment) error {
	commitment.NormalizeTrackLabel()
	note.DeleteFrontMatterField("track")
	for key, value := range map[string]interface{}{
		"kind":             commitment.Kind,
		"title":            commitment.Title,
		"sphere":           commitment.Sphere,
		"status":           commitment.Status,
		"outcome":          commitment.Outcome,
		"next_action":      commitment.NextAction,
		"context":          commitment.Context,
		"follow_up":        commitment.FollowUp,
		"due":              commitment.Due,
		"actor":            commitment.Actor,
		"waiting_for":      commitment.WaitingFor,
		"project":          commitment.Project,
		"last_evidence_at": commitment.LastEvidenceAt,
		"review_state":     commitment.ReviewState,
		"people":           commitment.People,
		"labels":           commitment.Labels,
	} {
		if err := note.SetFrontMatterField(key, value); err != nil {
			return err
		}
	}
	return nil
}

func overlayCommitment(base braingtd.Commitment, updates map[string]interface{}) (braingtd.Commitment, error) {
	out := base
	if v, ok := stringArgFromMap(updates, "title"); ok {
		out.Title = v
	}
	if v, ok := stringArgFromMap(updates, "kind"); ok {
		out.Kind = v
	}
	if v, ok := stringArgFromMap(updates, "sphere"); ok {
		out.Sphere = v
	}
	if v, ok := stringArgFromMap(updates, "status"); ok {
		out.Status = v
	}
	if v, ok := stringArgFromMap(updates, "outcome"); ok {
		out.Outcome = v
	}
	if v, ok := stringArgFromMap(updates, "next_action"); ok {
		out.NextAction = v
	}
	if v, ok := stringArgFromMap(updates, "context"); ok {
		out.Context = v
	}
	if v, ok := stringArgFromMap(updates, "follow_up"); ok {
		out.FollowUp = v
	}
	if v, ok := stringArgFromMap(updates, "due"); ok {
		out.Due = v
	}
	if v, ok := stringArgFromMap(updates, "actor"); ok {
		out.Actor = v
	}
	if v, ok := stringArgFromMap(updates, "waiting_for"); ok {
		out.WaitingFor = v
	}
	if v, ok := stringArgFromMap(updates, "project"); ok {
		out.Project = v
	}
	if v, ok := stringArgFromMap(updates, "track"); ok {
		out.Labels = braingtd.WithTrackLabel(out.Labels, v)
		out.Track = ""
	}
	if v, ok := stringArgFromMap(updates, "last_evidence_at"); ok {
		out.LastEvidenceAt = v
	}
	if v, ok := stringArgFromMap(updates, "review_state"); ok {
		out.ReviewState = v
	}
	if v, ok := stringSliceArgFromMap(updates, "people"); ok {
		out.People = v
	}
	if v, ok := stringSliceArgFromMap(updates, "labels"); ok {
		out.Labels = v
	}
	if raw, ok := updates["source_bindings"]; ok {
		bindings, err := sourceBindingsFromAny(raw)
		if err != nil {
			return braingtd.Commitment{}, err
		}
		out.SourceBindings = bindings
	}
	if raw, ok := updates["local_overlay"]; ok {
		overlay, err := decodeAny[braingtd.LocalOverlay](raw)
		if err != nil {
			return braingtd.Commitment{}, err
		}
		out.LocalOverlay = overlay
	}
	if raw, ok := updates["dedup"]; ok {
		dedup, err := decodeAny[braingtd.DedupState](raw)
		if err != nil {
			return braingtd.Commitment{}, err
		}
		out.Dedup = dedup
	}
	if v, ok := stringSliceArgFromMap(updates, "legacy_sources"); ok {
		out.LegacySources = v
	}
	return out, nil
}

func renderIngestCommitment(sphere, source, sourceRel string, task braincatalog.MeetingTask) string {
	sourceLabel := displayIngestSource(source)
	sourceName := strings.ToLower(strings.TrimSpace(source))
	sourceRef := sourceRel + "#" + strconv.Itoa(task.Line)
	return strings.TrimSpace(fmt.Sprintf(`---
kind: commitment
sphere: %s
title: %q
status: inbox
context: %s
source_bindings:
  - provider: %s
    ref: %q
    location:
      path: %s
      anchor: %q
---
# %s

## Summary
%s task from %s.

## Next Action
- [ ] %s

## Evidence
- %s#L%d

## Linked Items
- None.

## Review Notes
- Ingested from %s notes.
`, sphere, task.Text, sourceName, sourceName, sourceRef, sourceRel, task.Text, task.Text, sourceLabel, sourceRel, task.Text, sourceRel, task.Line, sourceLabel))
}
