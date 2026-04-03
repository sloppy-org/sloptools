package zotero

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func TestFindDefaultDatabaseUsesProfilesINI(t *testing.T) {
	home := t.TempDir()
	base := filepath.Join(home, ".zotero", "zotero")
	profileDir := filepath.Join(base, "abcd1234.default")
	if err := os.MkdirAll(profileDir, 0o755); err != nil {
		t.Fatalf("MkdirAll(profileDir): %v", err)
	}
	dbPath := filepath.Join(profileDir, "zotero.sqlite")
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("WriteFile(zotero.sqlite): %v", err)
	}
	profilesINI := "[Profile0]\nName=default\nIsRelative=1\nPath=abcd1234.default\nDefault=1\n"
	if err := os.WriteFile(filepath.Join(base, "profiles.ini"), []byte(profilesINI), 0o644); err != nil {
		t.Fatalf("WriteFile(profiles.ini): %v", err)
	}

	got, err := FindDefaultDatabase(home)
	if err != nil {
		t.Fatalf("FindDefaultDatabase() error: %v", err)
	}
	if got != dbPath {
		t.Fatalf("FindDefaultDatabase() = %q, want %q", got, dbPath)
	}
}

func TestFindDefaultDatabaseUsesMacPathFallback(t *testing.T) {
	home := t.TempDir()
	dbPath := filepath.Join(home, "Zotero", "zotero.sqlite")
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(Zotero): %v", err)
	}
	if err := os.WriteFile(dbPath, []byte("sqlite"), 0o644); err != nil {
		t.Fatalf("WriteFile(zotero.sqlite): %v", err)
	}

	got, err := FindDefaultDatabase(home)
	if err != nil {
		t.Fatalf("FindDefaultDatabase() error: %v", err)
	}
	if got != dbPath {
		t.Fatalf("FindDefaultDatabase() = %q, want %q", got, dbPath)
	}
}

func TestReaderListsLocalLibraryObjectsAndCitationKeys(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "zotero.sqlite")
	buildTestLibrary(t, dbPath)
	exportPath := filepath.Join(root, "library.json")
	exportJSON := `{"items":[{"itemKey":"ITEM-1","citationKey":"lovelace2026","DOI":"10.1000/example","title":"Pragmatic Testing"}]}`
	if err := os.WriteFile(exportPath, []byte(exportJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(library.json): %v", err)
	}

	reader, err := OpenReader(dbPath)
	if err != nil {
		t.Fatalf("OpenReader() error: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	ctx := context.Background()
	items, err := reader.ListItems(ctx)
	if err != nil {
		t.Fatalf("ListItems() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Key != "ITEM-1" || items[0].CitationKey != "lovelace2026" {
		t.Fatalf("items[0] = %#v", items[0])
	}
	if items[0].Journal != "Journal of Tests" {
		t.Fatalf("items[0].Journal = %q, want Journal of Tests", items[0].Journal)
	}
	if len(items[0].Creators) != 1 || items[0].Creators[0].LastName != "Lovelace" {
		t.Fatalf("items[0].Creators = %#v", items[0].Creators)
	}
	if len(items[0].Tags) != 1 || items[0].Tags[0] != "ml" {
		t.Fatalf("items[0].Tags = %#v", items[0].Tags)
	}
	if len(items[0].Collections) != 1 || items[0].Collections[0] != "COLL-1" {
		t.Fatalf("items[0].Collections = %#v", items[0].Collections)
	}

	attachments, err := reader.ListAttachments(ctx, "ITEM-1")
	if err != nil {
		t.Fatalf("ListAttachments() error: %v", err)
	}
	if len(attachments) != 1 || attachments[0].Key != "ATT-1" || attachments[0].ParentKey != "ITEM-1" {
		t.Fatalf("attachments = %#v", attachments)
	}
	if got := reader.AttachmentFileURL(attachments[0]); got != "file://"+filepath.ToSlash(filepath.Join(root, "storage", "ATT-1", "paper.pdf")) {
		t.Fatalf("AttachmentFileURL() = %q", got)
	}

	annotations, err := reader.ListAnnotations(ctx, "ATT-1")
	if err != nil {
		t.Fatalf("ListAnnotations() error: %v", err)
	}
	if len(annotations) != 1 || annotations[0].Key != "ANN-1" || annotations[0].ParentKey != "ATT-1" {
		t.Fatalf("annotations = %#v", annotations)
	}

	collections, err := reader.ListCollections(ctx)
	if err != nil {
		t.Fatalf("ListCollections() error: %v", err)
	}
	if len(collections) != 1 || collections[0].Key != "COLL-1" {
		t.Fatalf("collections = %#v", collections)
	}

	tags, err := reader.ListTags(ctx)
	if err != nil {
		t.Fatalf("ListTags() error: %v", err)
	}
	if len(tags) != 1 || tags[0].Name != "ml" || tags[0].ItemCount != 1 {
		t.Fatalf("tags = %#v", tags)
	}

	citationKeys, err := reader.ResolveCitationKeys()
	if err != nil {
		t.Fatalf("ResolveCitationKeys() error: %v", err)
	}
	if got := citationKeys["ITEM-1"]; got != "lovelace2026" {
		t.Fatalf("citationKeys[ITEM-1] = %q, want lovelace2026", got)
	}
}

func TestResolveCitationKeysFromBib(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "zotero.sqlite")
	buildTestLibrary(t, dbPath)
	exportPath := filepath.Join(root, "library.bib")
	exportBib := "@article{lovelace2026,\n  title={Pragmatic Testing},\n  doi={10.1000/example}\n}\n"
	if err := os.WriteFile(exportPath, []byte(exportBib), 0o644); err != nil {
		t.Fatalf("WriteFile(library.bib): %v", err)
	}

	reader, err := OpenReader(dbPath)
	if err != nil {
		t.Fatalf("OpenReader() error: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	citationKeys, err := reader.ResolveCitationKeys(exportPath)
	if err != nil {
		t.Fatalf("ResolveCitationKeys() error: %v", err)
	}
	if got := citationKeys["ITEM-1"]; got != "lovelace2026" {
		t.Fatalf("citationKeys[ITEM-1] = %q, want lovelace2026", got)
	}
}

func TestResolveCitationTextSupportsLaTeXAndPandoc(t *testing.T) {
	root := t.TempDir()
	dbPath := filepath.Join(root, "zotero.sqlite")
	buildTestLibrary(t, dbPath)
	exportPath := filepath.Join(root, "library.json")
	exportJSON := `{"items":[{"itemKey":"ITEM-1","citationKey":"lovelace2026","DOI":"10.1000/example","title":"Pragmatic Testing"}]}`
	if err := os.WriteFile(exportPath, []byte(exportJSON), 0o644); err != nil {
		t.Fatalf("WriteFile(library.json): %v", err)
	}

	reader, err := OpenReader(dbPath)
	if err != nil {
		t.Fatalf("OpenReader() error: %v", err)
	}
	t.Cleanup(func() { _ = reader.Close() })

	items, err := reader.ResolveCitationText(context.Background(), `See \cite{lovelace2026} and [@lovelace2026].`)
	if err != nil {
		t.Fatalf("ResolveCitationText() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("ResolveCitationText() len = %d, want 1", len(items))
	}
	if items[0].Key != "ITEM-1" || items[0].CitationKey != "lovelace2026" {
		t.Fatalf("ResolveCitationText() item = %#v", items[0])
	}
}

func buildTestLibrary(t *testing.T, dbPath string) {
	t.Helper()
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(dbPath))
	if err != nil {
		t.Fatalf("sql.Open(): %v", err)
	}
	t.Cleanup(func() { _ = db.Close() })

	statements := []string{
		`CREATE TABLE itemTypes (itemTypeID INTEGER PRIMARY KEY, typeName TEXT NOT NULL);`,
		`CREATE TABLE items (itemID INTEGER PRIMARY KEY, itemTypeID INTEGER NOT NULL, key TEXT NOT NULL, dateAdded TEXT NOT NULL DEFAULT '', dateModified TEXT NOT NULL DEFAULT '');`,
		`CREATE TABLE deletedItems (itemID INTEGER PRIMARY KEY);`,
		`CREATE TABLE fields (fieldID INTEGER PRIMARY KEY, fieldName TEXT NOT NULL);`,
		`CREATE TABLE itemDataValues (valueID INTEGER PRIMARY KEY, value TEXT NOT NULL);`,
		`CREATE TABLE itemData (itemID INTEGER NOT NULL, fieldID INTEGER NOT NULL, valueID INTEGER NOT NULL);`,
		`CREATE TABLE creatorTypes (creatorTypeID INTEGER PRIMARY KEY, creatorType TEXT NOT NULL);`,
		`CREATE TABLE creatorData (creatorDataID INTEGER PRIMARY KEY, firstName TEXT NOT NULL DEFAULT '', lastName TEXT NOT NULL DEFAULT '', name TEXT NOT NULL DEFAULT '');`,
		`CREATE TABLE creators (creatorID INTEGER PRIMARY KEY, creatorDataID INTEGER NOT NULL);`,
		`CREATE TABLE itemCreators (itemID INTEGER NOT NULL, creatorID INTEGER NOT NULL, creatorTypeID INTEGER NOT NULL, orderIndex INTEGER NOT NULL);`,
		`CREATE TABLE collections (collectionID INTEGER PRIMARY KEY, key TEXT NOT NULL, collectionName TEXT NOT NULL, parentCollectionID INTEGER);`,
		`CREATE TABLE collectionItems (collectionID INTEGER NOT NULL, itemID INTEGER NOT NULL);`,
		`CREATE TABLE tags (tagID INTEGER PRIMARY KEY, name TEXT NOT NULL);`,
		`CREATE TABLE itemTags (itemID INTEGER NOT NULL, tagID INTEGER NOT NULL, type INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE itemAttachments (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, contentType TEXT NOT NULL DEFAULT '', path TEXT NOT NULL DEFAULT '', linkMode INTEGER NOT NULL DEFAULT 0);`,
		`CREATE TABLE itemAnnotations (itemID INTEGER PRIMARY KEY, parentItemID INTEGER, type TEXT NOT NULL DEFAULT '', authorName TEXT NOT NULL DEFAULT '', text TEXT NOT NULL DEFAULT '', comment TEXT NOT NULL DEFAULT '', color TEXT NOT NULL DEFAULT '', pageLabel TEXT NOT NULL DEFAULT '', sortIndex TEXT NOT NULL DEFAULT '', position TEXT NOT NULL DEFAULT '');`,
		`INSERT INTO itemTypes (itemTypeID, typeName) VALUES (1, 'journalArticle'), (2, 'attachment'), (3, 'annotation');`,
		`INSERT INTO items (itemID, itemTypeID, key, dateAdded, dateModified) VALUES (1, 1, 'ITEM-1', '2026-03-08', '2026-03-09'), (2, 2, 'ATT-1', '2026-03-08', '2026-03-09'), (3, 3, 'ANN-1', '2026-03-08', '2026-03-09');`,
		`INSERT INTO fields (fieldID, fieldName) VALUES (1, 'title'), (2, 'DOI'), (3, 'abstractNote'), (4, 'date'), (5, 'publicationTitle');`,
		`INSERT INTO itemDataValues (valueID, value) VALUES (1, 'Pragmatic Testing'), (2, '10.1000/example'), (3, 'Short abstract.'), (4, '2026'), (5, 'Paper PDF'), (6, 'Journal of Tests');`,
		`INSERT INTO itemData (itemID, fieldID, valueID) VALUES (1, 1, 1), (1, 2, 2), (1, 3, 3), (1, 4, 4), (1, 5, 6), (2, 1, 5);`,
		`INSERT INTO creatorTypes (creatorTypeID, creatorType) VALUES (1, 'author');`,
		`INSERT INTO creatorData (creatorDataID, firstName, lastName, name) VALUES (1, 'Ada', 'Lovelace', '');`,
		`INSERT INTO creators (creatorID, creatorDataID) VALUES (1, 1);`,
		`INSERT INTO itemCreators (itemID, creatorID, creatorTypeID, orderIndex) VALUES (1, 1, 1, 0);`,
		`INSERT INTO collections (collectionID, key, collectionName, parentCollectionID) VALUES (1, 'COLL-1', 'Papers', NULL);`,
		`INSERT INTO collectionItems (collectionID, itemID) VALUES (1, 1);`,
		`INSERT INTO tags (tagID, name) VALUES (1, 'ml');`,
		`INSERT INTO itemTags (itemID, tagID, type) VALUES (1, 1, 0);`,
		`INSERT INTO itemAttachments (itemID, parentItemID, contentType, path, linkMode) VALUES (2, 1, 'application/pdf', 'storage:paper.pdf', 1);`,
		`INSERT INTO itemAnnotations (itemID, parentItemID, type, authorName, text, comment, color, pageLabel, sortIndex, position) VALUES (3, 2, 'highlight', 'Ada', 'Important result', 'Revisit this proof', '#ffd400', '4', '00001|00001|00001', '{\"pageIndex\":3}');`,
	}
	for _, stmt := range statements {
		if _, err := db.Exec(stmt); err != nil {
			t.Fatalf("db.Exec(%q): %v", stmt, err)
		}
	}
	storagePath := filepath.Join(filepath.Dir(dbPath), "storage", "ATT-1")
	if err := os.MkdirAll(storagePath, 0o755); err != nil {
		t.Fatalf("MkdirAll(storage): %v", err)
	}
	if err := os.WriteFile(filepath.Join(storagePath, "paper.pdf"), []byte("%PDF-1.7"), 0o644); err != nil {
		t.Fatalf("WriteFile(paper.pdf): %v", err)
	}
}
