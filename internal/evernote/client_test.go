package evernote

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(
		"token-1",
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return client
}

func TestTokenEnvVarAndNewClientFromEnv(t *testing.T) {
	if got, want := TokenEnvVar("Lab Notes"), "SLOPSHELL_EVERNOTE_TOKEN_LAB_NOTES"; got != want {
		t.Fatalf("TokenEnvVar() = %q, want %q", got, want)
	}

	t.Setenv(TokenEnvVar("Lab Notes"), "token-xyz")
	client, err := NewClientFromEnv("Lab Notes")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error: %v", err)
	}
	if client.token != "token-xyz" {
		t.Fatalf("token = %q, want token-xyz", client.token)
	}

	if _, err := NewClientFromEnv("Missing"); !errors.Is(err, ErrTokenNotConfigured) {
		t.Fatalf("NewClientFromEnv(missing) error = %v, want ErrTokenNotConfigured", err)
	}
}

func TestListEndpointsAndNoteDecoding(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer token-1" {
			t.Fatalf("Authorization = %q, want Bearer token-1", got)
		}
		switch r.URL.Path {
		case "/notebooks":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"notebooks": []map[string]any{{
					"id":         "nb-1",
					"name":       "Research",
					"stack":      "Work",
					"updated_at": "2026-03-08T12:00:00Z",
				}},
			})
		case "/notes":
			if got := r.URL.Query().Get("notebook_id"); got != "nb-1" {
				t.Fatalf("notebook_id = %q, want nb-1", got)
			}
			if got := r.URL.Query().Get("query"); got != "fusion" {
				t.Fatalf("query = %q, want fusion", got)
			}
			if got := r.URL.Query().Get("limit"); got != "10" {
				t.Fatalf("limit = %q, want 10", got)
			}
			_ = json.NewEncoder(w).Encode(map[string]any{
				"notes": []map[string]any{{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "EUROfusion summary",
					"tag_names":   []string{"fusion", "reading"},
					"content":     `<en-note><div>Latest results</div></en-note>`,
				}},
			})
		case "/notes/note-1":
			_ = json.NewEncoder(w).Encode(map[string]any{
				"note": map[string]any{
					"id":          "note-1",
					"notebook_id": "nb-1",
					"title":       "EUROfusion summary",
					"created_at":  "2026-03-01T09:00:00Z",
					"updated_at":  "2026-03-08T12:30:00Z",
					"tag_names":   []string{"fusion", "reading"},
					"content_enml": `<en-note>` +
						`<div>Review section 2</div>` +
						`<div><en-todo checked="true"/>Check bibliography</div>` +
						`<div><en-todo/>Draft summary paragraph</div>` +
						`</en-note>`,
				},
			})
		case "/tags":
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"id":        "tag-1",
				"name":      "fusion",
				"parent_id": "tag-root",
			}})
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	})

	notebooks, err := client.ListNotebooks(context.Background())
	if err != nil {
		t.Fatalf("ListNotebooks() error: %v", err)
	}
	if len(notebooks) != 1 || notebooks[0].ID != "nb-1" || notebooks[0].Stack != "Work" {
		t.Fatalf("notebooks = %#v", notebooks)
	}

	notes, err := client.ListNotes(context.Background(), "nb-1", ListNotesOptions{
		Query: "fusion",
		Limit: 10,
	})
	if err != nil {
		t.Fatalf("ListNotes() error: %v", err)
	}
	if len(notes) != 1 || notes[0].Title != "EUROfusion summary" || notes[0].ContentText != "Latest results" {
		t.Fatalf("notes = %#v", notes)
	}

	note, err := client.GetNote(context.Background(), "note-1")
	if err != nil {
		t.Fatalf("GetNote() error: %v", err)
	}
	if note.ContentENML == "" || note.ContentText == "" || note.ContentMarkdown == "" {
		t.Fatalf("note content not decoded: %#v", note)
	}
	if len(note.Tasks) != 2 {
		t.Fatalf("tasks len = %d, want 2", len(note.Tasks))
	}
	if !note.Tasks[0].Checked || note.Tasks[0].Text != "Check bibliography" {
		t.Fatalf("first task = %#v", note.Tasks[0])
	}
	if note.Tasks[1].Checked || note.Tasks[1].Text != "Draft summary paragraph" {
		t.Fatalf("second task = %#v", note.Tasks[1])
	}

	tags, err := client.ListTags(context.Background())
	if err != nil {
		t.Fatalf("ListTags() error: %v", err)
	}
	if len(tags) != 1 || tags[0].ParentID != "tag-root" {
		t.Fatalf("tags = %#v", tags)
	}
}

func TestAPIErrorAndValidation(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})

	if _, err := client.ListTags(context.Background()); err == nil {
		t.Fatal("ListTags() error = nil, want APIError")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("ListTags() error = %v, want APIError 429", err)
		}
	}

	if _, err := client.GetNote(context.Background(), ""); !errors.Is(err, ErrNoteIDRequired) {
		t.Fatalf("GetNote(empty) error = %v, want ErrNoteIDRequired", err)
	}
}

func TestConvertENMLToText(t *testing.T) {
	text, markdown, tasks := ConvertENMLToText(
		`<en-note><div>Alpha</div><div><en-todo checked="checked"/>Beta task</div><ul><li>Gamma</li></ul></en-note>`,
	)

	if text != "Alpha\n\n[x] Beta task\n\n- Gamma" {
		t.Fatalf("text = %q", text)
	}
	if markdown != "Alpha\n\n[x] Beta task\n\n- Gamma" {
		t.Fatalf("markdown = %q", markdown)
	}
	if len(tasks) != 1 || tasks[0].Text != "Beta task" || !tasks[0].Checked {
		t.Fatalf("tasks = %#v", tasks)
	}
}
