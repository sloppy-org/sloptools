package bear

import (
	"context"
	"database/sql"
	"errors"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestClientListNotesExtractsTagsAndChecklist(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "bear.sqlite")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	t.Cleanup(func() {
		_ = db.Close()
	})
	if _, err := db.Exec(`
CREATE TABLE ZSFNOTE (
  Z_PK INTEGER PRIMARY KEY,
  ZUNIQUEIDENTIFIER TEXT,
  ZTITLE TEXT,
  ZTEXT TEXT,
  ZCREATIONDATE REAL,
  ZMODIFICATIONDATE REAL,
  ZTRASHED INTEGER DEFAULT 0,
  ZARCHIVED INTEGER DEFAULT 0
);
INSERT INTO ZSFNOTE (ZUNIQUEIDENTIFIER, ZTITLE, ZTEXT, ZCREATIONDATE, ZMODIFICATIONDATE, ZTRASHED, ZARCHIVED)
VALUES
  ('note-1', 'Reading queue', '#Slopshell' || char(10) || '- [ ] Review intro' || char(10) || '- [x] Fix refs', 788918400, 788922000, 0, 0),
  ('note-2', 'Archived', '#Skip', 788918400, 788922000, 0, 1),
  ('note-3', 'Trashed', '#Skip', 788918400, 788922000, 1, 0);
`); err != nil {
		t.Fatalf("seed bear schema: %v", err)
	}

	client, err := NewClient(dbPath)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	notes, err := client.ListNotes(context.Background())
	if err != nil {
		t.Fatalf("ListNotes() error: %v", err)
	}
	if len(notes) != 1 {
		t.Fatalf("ListNotes() len = %d, want 1", len(notes))
	}
	note := notes[0]
	if note.ID != "note-1" {
		t.Fatalf("note.ID = %q, want note-1", note.ID)
	}
	if note.Created != "2026-01-01T00:00:00Z" {
		t.Fatalf("note.Created = %q, want 2026-01-01T00:00:00Z", note.Created)
	}
	if note.Modified != "2026-01-01T01:00:00Z" {
		t.Fatalf("note.Modified = %q, want 2026-01-01T01:00:00Z", note.Modified)
	}
	if len(note.Tags) != 1 || note.Tags[0] != "Slopshell" {
		t.Fatalf("note.Tags = %#v, want [Slopshell]", note.Tags)
	}

	checklist := ExtractChecklist(note.Markdown)
	if len(checklist) != 2 {
		t.Fatalf("ExtractChecklist() len = %d, want 2", len(checklist))
	}
	if checklist[0].Text != "Review intro" || checklist[0].Checked {
		t.Fatalf("checklist[0] = %#v", checklist[0])
	}
	if checklist[1].Text != "Fix refs" || !checklist[1].Checked {
		t.Fatalf("checklist[1] = %#v", checklist[1])
	}
}

func TestClientListNotesMissingDatabase(t *testing.T) {
	client, err := NewClient(filepath.Join(t.TempDir(), "missing.sqlite"))
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	_, err = client.ListNotes(context.Background())
	if !errors.Is(err, ErrDatabaseNotFound) {
		t.Fatalf("ListNotes() error = %v, want ErrDatabaseNotFound", err)
	}
}
