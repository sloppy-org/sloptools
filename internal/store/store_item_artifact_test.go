package store

import (
	"database/sql"
	"path/filepath"
	"testing"
)

func TestStoreMigratesLegacyPrimaryArtifactIntoItemArtifactLinks(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "slopshell.db")
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		t.Fatalf("sql.Open() error: %v", err)
	}
	schema := `
CREATE TABLE workspaces (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  dir_path TEXT NOT NULL UNIQUE,
  is_active INTEGER NOT NULL DEFAULT 0,
  is_daily INTEGER NOT NULL DEFAULT 0,
  daily_date TEXT,
  mcp_url TEXT NOT NULL DEFAULT '',
  canvas_session_id TEXT NOT NULL DEFAULT '',
  chat_model TEXT NOT NULL DEFAULT '',
  chat_model_reasoning_effort TEXT NOT NULL DEFAULT '',
  companion_config_json TEXT NOT NULL DEFAULT '{}',
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE actors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  kind TEXT NOT NULL,
  created_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE artifacts (
  id INTEGER PRIMARY KEY,
  kind TEXT NOT NULL,
  ref_path TEXT,
  ref_url TEXT,
  title TEXT,
  meta_json TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
CREATE TABLE items (
  id INTEGER PRIMARY KEY,
  title TEXT NOT NULL,
  state TEXT NOT NULL DEFAULT 'inbox' CHECK (state IN ('inbox', 'waiting', 'someday', 'done')),
  workspace_id INTEGER REFERENCES workspaces(id) ON DELETE SET NULL,
  artifact_id INTEGER REFERENCES artifacts(id) ON DELETE SET NULL,
  actor_id INTEGER REFERENCES actors(id) ON DELETE SET NULL,
  visible_after TEXT,
  follow_up_at TEXT,
  source TEXT,
  source_ref TEXT,
  created_at TEXT NOT NULL DEFAULT (datetime('now')),
  updated_at TEXT NOT NULL DEFAULT (datetime('now'))
);
INSERT INTO artifacts (id, kind, title) VALUES (7, 'markdown', 'Legacy note');
INSERT INTO items (id, title, artifact_id) VALUES (9, 'Legacy item', 7);
`
	if _, err := db.Exec(schema); err != nil {
		_ = db.Close()
		t.Fatalf("seed schema: %v", err)
	}
	if err := db.Close(); err != nil {
		t.Fatalf("close seeded db: %v", err)
	}

	s, err := New(dbPath)
	if err != nil {
		t.Fatalf("store.New() error: %v", err)
	}
	t.Cleanup(func() {
		_ = s.Close()
	})

	links, err := s.ListItemArtifactLinks(9)
	if err != nil {
		t.Fatalf("ListItemArtifactLinks() error: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("ListItemArtifactLinks() len = %d, want 1", len(links))
	}
	if links[0].ArtifactID != 7 || links[0].Role != "source" {
		t.Fatalf("ListItemArtifactLinks() = %+v, want source link to artifact 7", links)
	}
}

func TestStoreItemArtifactLinkLifecycle(t *testing.T) {
	s := newTestStore(t)

	sourceTitle := "Source brief"
	sourceArtifact, err := s.CreateArtifact(ArtifactKindMarkdown, nil, nil, &sourceTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(source) error: %v", err)
	}
	relatedTitle := "Related email"
	relatedArtifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &relatedTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(related) error: %v", err)
	}
	outputTitle := "Output PDF"
	outputArtifact, err := s.CreateArtifact(ArtifactKindPDF, nil, nil, &outputTitle, nil)
	if err != nil {
		t.Fatalf("CreateArtifact(output) error: %v", err)
	}

	item, err := s.CreateItem("Review supporting material", ItemOptions{ArtifactID: &sourceArtifact.ID})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	reviewTarget := ItemReviewTargetGitHub
	reviewer := "octocat"
	if err := s.UpdateItemReviewDispatch(item.ID, &reviewTarget, &reviewer); err != nil {
		t.Fatalf("UpdateItemReviewDispatch() error: %v", err)
	}

	initialArtifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(initial) error: %v", err)
	}
	if len(initialArtifacts) != 1 || initialArtifacts[0].ArtifactID != sourceArtifact.ID || initialArtifacts[0].Role != "source" {
		t.Fatalf("ListItemArtifacts(initial) = %+v, want source artifact %d", initialArtifacts, sourceArtifact.ID)
	}

	if err := s.LinkItemArtifact(item.ID, relatedArtifact.ID, "related"); err != nil {
		t.Fatalf("LinkItemArtifact(related) error: %v", err)
	}
	if err := s.LinkItemArtifact(item.ID, outputArtifact.ID, "source"); err != nil {
		t.Fatalf("LinkItemArtifact(source) error: %v", err)
	}

	updatedItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(updated) error: %v", err)
	}
	if updatedItem.ArtifactID == nil || *updatedItem.ArtifactID != outputArtifact.ID {
		t.Fatalf("GetItem(updated).ArtifactID = %v, want %d", updatedItem.ArtifactID, outputArtifact.ID)
	}

	artifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(updated) error: %v", err)
	}
	if len(artifacts) != 3 {
		t.Fatalf("ListItemArtifacts(updated) len = %d, want 3", len(artifacts))
	}
	if artifacts[0].ArtifactID != outputArtifact.ID || artifacts[0].Role != "source" {
		t.Fatalf("primary artifact = %+v, want output artifact as source", artifacts[0])
	}

	itemsForRelated, err := s.ListArtifactItems(relatedArtifact.ID)
	if err != nil {
		t.Fatalf("ListArtifactItems() error: %v", err)
	}
	if len(itemsForRelated) != 1 || itemsForRelated[0].ID != item.ID {
		t.Fatalf("ListArtifactItems() = %+v, want item %d", itemsForRelated, item.ID)
	}
	if itemsForRelated[0].ReviewTarget == nil || *itemsForRelated[0].ReviewTarget != reviewTarget {
		t.Fatalf("ListArtifactItems().ReviewTarget = %v, want %q", itemsForRelated[0].ReviewTarget, reviewTarget)
	}
	if itemsForRelated[0].Reviewer == nil || *itemsForRelated[0].Reviewer != reviewer {
		t.Fatalf("ListArtifactItems().Reviewer = %v, want %q", itemsForRelated[0].Reviewer, reviewer)
	}
	if itemsForRelated[0].ReviewedAt == nil || *itemsForRelated[0].ReviewedAt == "" {
		t.Fatalf("ListArtifactItems().ReviewedAt = %v, want timestamp", itemsForRelated[0].ReviewedAt)
	}

	if err := s.UnlinkItemArtifact(item.ID, outputArtifact.ID); err != nil {
		t.Fatalf("UnlinkItemArtifact(primary) error: %v", err)
	}

	restoredItem, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(restored) error: %v", err)
	}
	if restoredItem.ArtifactID == nil || *restoredItem.ArtifactID != sourceArtifact.ID {
		t.Fatalf("GetItem(restored).ArtifactID = %v, want %d", restoredItem.ArtifactID, sourceArtifact.ID)
	}

	restoredArtifacts, err := s.ListItemArtifacts(item.ID)
	if err != nil {
		t.Fatalf("ListItemArtifacts(restored) error: %v", err)
	}
	if len(restoredArtifacts) != 2 {
		t.Fatalf("ListItemArtifacts(restored) len = %d, want 2", len(restoredArtifacts))
	}
	if restoredArtifacts[0].ArtifactID != sourceArtifact.ID || restoredArtifacts[0].Role != "source" {
		t.Fatalf("restored primary artifact = %+v, want source artifact %d", restoredArtifacts[0], sourceArtifact.ID)
	}
}
