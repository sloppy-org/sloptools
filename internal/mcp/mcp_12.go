package mcp

import (
	"context"
	"errors"
	"fmt"
	"strings"

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
