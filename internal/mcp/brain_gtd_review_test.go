package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/brain/gtd/today"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

func TestBrainGTDReviewListMailBranchIsLive(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain", "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Work Mail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	day0 := time.Date(2026, time.May, 4, 9, 0, 0, 0, time.UTC)
	due := day0.AddDate(0, 0, 7)
	askBody := "Could you send the report by 2026-05-08?"
	provider := &fakeMailProvider{
		listIDs: []string{"unread-inbox", "read-inbox", "deferred-flag", "waiting", "archived"},
		messages: map[string]*providerdata.EmailMessage{
			"unread-inbox":  {ID: "unread-inbox", Subject: "Unread", Sender: "Ada <ada@example.com>", Date: day0, Folder: "INBOX", BodyText: &askBody, Snippet: "ask"},
			"read-inbox":    {ID: "read-inbox", Subject: "Read", Sender: "Ada <ada@example.com>", Date: day0.Add(time.Hour), Folder: "INBOX", IsRead: true, BodyText: &askBody, Snippet: "ask"},
			"deferred-flag": {ID: "deferred-flag", Subject: "Deferred", Sender: "Ada <ada@example.com>", Date: day0.Add(2 * time.Hour), Folder: "INBOX", IsRead: true, IsFlagged: true, FollowUpAt: &due, BodyText: &askBody, Snippet: "ask"},
			"waiting":       {ID: "waiting", Subject: "Waiting", Sender: "Ada <ada@example.com>", Date: day0.Add(3 * time.Hour), Folder: "Waiting", IsRead: true, BodyText: &askBody, Snippet: "ask"},
			"archived":      {ID: "archived", Subject: "Archived", Sender: "Ada <ada@example.com>", Date: day0.Add(4 * time.Hour), Folder: "Archive", IsRead: true, BodyText: &askBody, Snippet: "ask"},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"sphere":  "work",
		"sources": []interface{}{"mail"},
	})
	if err != nil {
		t.Fatalf("review_list mail: %v", err)
	}
	items, _ := got["items"].([]gtdReviewItem)
	byID := map[string]gtdReviewItem{}
	for _, item := range items {
		if item.Source != "mail" {
			t.Fatalf("non-mail item leaked into mail-only request: %+v", item)
		}
		byID[fmt.Sprintf("mail:work:%d:%s", account.ID, mailRecordSourceID(item.SourceRef))] = item
	}
	wantStatus := map[string]string{
		"unread-inbox":  "inbox",
		"read-inbox":    "next",
		"deferred-flag": "deferred",
		"waiting":       "waiting",
		"archived":      "closed",
	}
	for messageID, want := range wantStatus {
		ref := fmt.Sprintf("mail:work:%d:%s", account.ID, messageID)
		got, ok := byID[ref]
		if !ok {
			t.Fatalf("missing mail item %s", ref)
			continue
		}
		if got.Status != want {
			t.Fatalf("status for %s = %q, want %q", messageID, got.Status, want)
		}
	}
}

func mailRecordSourceID(ref string) string {
	idx := -1
	parts := []byte(ref)
	colons := 0
	for i, b := range parts {
		if b == ':' {
			colons++
			if colons == 3 {
				idx = i + 1
				break
			}
		}
	}
	if idx < 0 || idx >= len(ref) {
		return ref
	}
	return ref[idx:]
}

func TestBrainGTDReviewListDefaultSourcesExcludeTasks(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	if err := os.MkdirAll(filepath.Join(tmp, "work", "brain", "projects"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	s, st, _ := newDomainServerForTest(t)
	s.brainConfigPath = configPath
	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	called := 0
	s.newTasksProvider = func(context.Context, store.ExternalAccount) (tasks.Provider, error) {
		called++
		return nil, fmt.Errorf("tasks provider must not be called")
	}
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("review_list default: %v", err)
	}
	if called != 0 {
		t.Fatalf("tasks provider called %d times for default review_list", called)
	}
	errs, _ := got["errors"].([]string)
	for _, msg := range errs {
		if msg == "tasks source is deprecated as a default; pass explicit sources=['tasks'] to opt in" {
			t.Fatalf("default review_list should not emit tasks deprecation: %v", errs)
		}
	}
	got, err = s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"sources":     []interface{}{"tasks"},
	})
	if err != nil {
		t.Fatalf("review_list explicit tasks: %v", err)
	}
	errs, _ = got["errors"].([]string)
	foundDeprecation := false
	for _, msg := range errs {
		if msg == "tasks source is deprecated as a default; pass explicit sources=['tasks'] to opt in" {
			foundDeprecation = true
		}
	}
	if !foundDeprecation {
		t.Fatalf("explicit tasks source should emit deprecation, errors=%v", errs)
	}
}

func TestBrainGTDReviewListDedupsIssueShadows(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/commitments/github/issue-43.md", "Add GTD tools", "github", "sloppy-org/sloptools#43")
	s := NewServer(t.TempDir())
	build := gtdReviewBuild{bindings: map[string]string{}, seen: map[string]struct{}{}}
	if err := s.addMarkdownGTDItems(map[string]interface{}{"config_path": configPath, "sphere": "work"}, &build); err != nil {
		t.Fatalf("addMarkdownGTDItems: %v", err)
	}
	if len(build.items) != 1 {
		t.Fatalf("markdown items = %d, want 1", len(build.items))
	}
	openIssue := providerdata.SourceItem{Provider: "github", Kind: "issue", Container: "sloppy-org/sloptools", Number: 43, Title: "Add GTD tools", State: "OPEN", SourceRef: "github:sloppy-org/sloptools#43"}
	build.addOrSkipExisting(gtdReviewItemFromSourceItem(openIssue))
	if len(build.items) != 1 {
		t.Fatalf("after open API issue, items = %d, want 1", len(build.items))
	}
	if build.items[0].Source != "github" {
		t.Fatalf("open issue did not displace markdown: %+v", build.items[0])
	}
	build = gtdReviewBuild{bindings: map[string]string{}, seen: map[string]struct{}{}}
	if err := s.addMarkdownGTDItems(map[string]interface{}{"config_path": configPath, "sphere": "work"}, &build); err != nil {
		t.Fatalf("addMarkdownGTDItems re-run: %v", err)
	}
	closedIssue := providerdata.SourceItem{Provider: "github", Kind: "issue", Container: "sloppy-org/sloptools", Number: 43, Title: "Add GTD tools", State: "closed", SourceRef: "github:sloppy-org/sloptools#43"}
	build.addOrSkipExisting(gtdReviewItemFromSourceItem(closedIssue))
	if len(build.items) != 1 {
		t.Fatalf("after closed API issue, items = %d, want 1 (markdown wins)", len(build.items))
	}
	if build.items[0].Source != "markdown" {
		t.Fatalf("closed API issue displaced markdown: %+v", build.items[0])
	}
}

func TestBrainGTDTracksIncludesWIPFieldsFromConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	gtdConfig := filepath.Join(tmp, "gtd.toml")
	if err := os.WriteFile(gtdConfig, []byte(`[[track]]
sphere = "work"
name = "research"
wip_limit = 2
`), 0o644); err != nil {
		t.Fatalf("write gtd.toml: %v", err)
	}
	// Mix next and in_progress: per issue #89 both statuses must count
	// toward open_wip_count and the wip_status classification.
	statuses := []string{"next", "in_progress", "in_progress"}
	for i, title := range []string{"A", "B", "C"} {
		writeTrackedCommitment(t, tmp, fmt.Sprintf("brain/gtd/research-%d.md", i), title, statuses[i], "research")
	}
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.tracks", map[string]interface{}{
		"config_path": configPath,
		"gtd_config":  gtdConfig,
		"sphere":      "work",
	})
	if err != nil {
		t.Fatalf("brain.gtd.tracks: %v", err)
	}
	tracks, _ := got["tracks"].([]map[string]interface{})
	if len(tracks) != 1 {
		t.Fatalf("tracks = %#v, want one entry", tracks)
	}
	row := tracks[0]
	if row["id"] != "research" {
		t.Fatalf("track id = %v", row["id"])
	}
	if row["wip_limit"] != 2 {
		t.Fatalf("wip_limit = %v, want 2", row["wip_limit"])
	}
	if row["wip_status"] != "over" {
		t.Fatalf("wip_status = %v, want over", row["wip_status"])
	}
	if row["open_wip_count"] != 3 {
		t.Fatalf("open_wip_count = %v, want 3", row["open_wip_count"])
	}
}

func TestBrainGTDReviewListReturnsOverWIPWhenActiveExceedsLimit(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	gtdConfig := filepath.Join(tmp, "gtd.toml")
	if err := os.WriteFile(gtdConfig, []byte(`[[track]]
sphere = "work"
name = "research"
wip_limit = 2

[[track]]
sphere = "work"
name = "teaching"
wip_limit = 5
`), 0o644); err != nil {
		t.Fatalf("write gtd.toml: %v", err)
	}
	// Mix next and in_progress to exercise the issue #89 regression: WIP
	// counting must include both statuses, not next alone.
	statuses := []string{"next", "in_progress", "in_progress"}
	for i, title := range []string{"Alpha", "Beta", "Gamma"} {
		writeTrackedCommitment(t, tmp, fmt.Sprintf("brain/gtd/research-%d.md", i), title, statuses[i], "research")
	}
	writeTrackedCommitment(t, tmp, "brain/gtd/research-waiting.md", "Wait", "waiting", "research")
	writeTrackedCommitment(t, tmp, "brain/gtd/teaching-1.md", "Teach", "next", "teaching")
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"gtd_config":  gtdConfig,
		"sphere":      "work",
		"sources":     []interface{}{"markdown"},
	})
	if err != nil {
		t.Fatalf("review_list: %v", err)
	}
	overWIP, _ := got["over_wip"].([]string)
	if len(overWIP) != 1 || overWIP[0] != "research" {
		t.Fatalf("over_wip = %v, want [research]", overWIP)
	}
}

func TestBrainGTDReviewListOverWIPEmptyWhenNoConfig(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeTrackedCommitment(t, tmp, "brain/gtd/x.md", "X", "next", "research")
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"gtd_config":  filepath.Join(tmp, "missing.toml"),
		"sphere":      "work",
		"sources":     []interface{}{"markdown"},
	})
	if err != nil {
		t.Fatalf("review_list: %v", err)
	}
	overWIP, ok := got["over_wip"].([]string)
	if !ok {
		t.Fatalf("over_wip missing or wrong type: %v", got["over_wip"])
	}
	if len(overWIP) != 0 {
		t.Fatalf("over_wip should be empty without config, got %v", overWIP)
	}
}

func writeTrackedCommitment(t *testing.T, root, rel, title, status, track string) {
	t.Helper()
	body := fmt.Sprintf(`---
kind: commitment
sphere: work
title: %q
status: %s
context: review
next_action: Move forward
outcome: %q
labels:
  - track/%s
---
# %s

## Summary
Move forward.

## Next Action
- [ ] Move forward

## Evidence
- none

## Linked Items
- None.

## Review Notes
- None.
`, title, status, title, track, title)
	writeMCPBrainFile(t, filepath.Join(root, "work", filepath.FromSlash(rel)), body)
}

func TestBrainGTDReviewListExcludesShadowMailMarkdown(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/commitments/mail/m1.md", "Reply to Ada", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/manual.md", "Manual commitment", "manual", "m1")
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"sources":     []interface{}{"markdown"},
	})
	if err != nil {
		t.Fatalf("review_list markdown only: %v", err)
	}
	items, _ := got["items"].([]gtdReviewItem)
	for _, item := range items {
		if item.Path == "brain/commitments/mail/m1.md" {
			t.Fatalf("shadow mail markdown surfaced: %+v", item)
		}
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want only the manual commitment: %+v", len(items), items)
	}
}

// TestBrainGTDTodayCapsAtEightAndPersists exercises the closed-daily-list
// guarantee: at most eight items, written under brain/gtd/today/<date>.md,
// validated by the same Markdown writer as other GTD mutations.
func TestBrainGTDTodayCapsAtEightAndPersists(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	for i := 0; i < 12; i++ {
		writeDedupCommitment(t, tmp, "work",
			fmt.Sprintf("brain/gtd/c-%02d.md", i),
			fmt.Sprintf("Outcome %02d", i),
			"manual", fmt.Sprintf("ref-%02d", i))
	}
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.today", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"date":        "2026-05-04",
	})
	if err != nil {
		t.Fatalf("brain.gtd.today: %v", err)
	}
	if got["count"] != gtdtoday.HardItemCap {
		t.Fatalf("count=%v, want %d", got["count"], gtdtoday.HardItemCap)
	}
	rel, _ := got["path"].(string)
	if rel == "" || filepath.ToSlash(rel) != "brain/gtd/today/2026-05-04.md" {
		t.Fatalf("path=%q, want brain/gtd/today/2026-05-04.md", rel)
	}
	abs := filepath.Join(tmp, "work", filepath.FromSlash(rel))
	data, err := os.ReadFile(abs)
	if err != nil {
		t.Fatalf("read closed list: %v", err)
	}
	if diags := brain.ValidateMarkdownNote(string(data), brain.MarkdownParseOptions{}); len(diags) != 0 {
		t.Fatalf("closed list invalid: %#v\n%s", diags, string(data))
	}
	if got["frozen"] != true || got["updated"] != true {
		t.Fatalf("frozen/updated=%v/%v, want true/true", got["frozen"], got["updated"])
	}
}

// TestBrainGTDTodayClosesTheListForLateAdditions captures the regression the
// issue calls out: anything inserted after the day's closed list is generated
// must NOT appear in that same day's list.
func TestBrainGTDTodayClosesTheListForLateAdditions(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/early-1.md", "Early one", "manual", "ref-1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/early-2.md", "Early two", "manual", "ref-2")

	s := NewServer(t.TempDir())
	first, err := s.callTool("brain.gtd.today", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"date":        "2026-05-04",
	})
	if err != nil {
		t.Fatalf("first today: %v", err)
	}
	if first["count"] != 2 {
		t.Fatalf("first count=%v, want 2", first["count"])
	}

	writeDedupCommitment(t, tmp, "work", "brain/gtd/late.md", "Late arrival", "manual", "ref-3")

	second, err := s.callTool("brain.gtd.today", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"date":        "2026-05-04",
	})
	if err != nil {
		t.Fatalf("second today: %v", err)
	}
	if second["count"] != 2 {
		t.Fatalf("second count=%v, want 2 (closed list must not absorb late commitments)", second["count"])
	}
	if second["updated"] != false || second["frozen"] != true {
		t.Fatalf("second updated/frozen=%v/%v, want false/true", second["updated"], second["frozen"])
	}
	items, _ := second["items"].([]gtdtoday.Item)
	for _, item := range items {
		if item.Path == "brain/gtd/late.md" {
			t.Fatalf("late commitment leaked into closed list: %+v", item)
		}
	}

	refreshed, err := s.callTool("brain.gtd.today", map[string]interface{}{
		"config_path": configPath,
		"sphere":      "work",
		"date":        "2026-05-04",
		"refresh":     true,
	})
	if err != nil {
		t.Fatalf("refresh today: %v", err)
	}
	if refreshed["count"] != 3 {
		t.Fatalf("refresh count=%v, want 3 (refresh must include the late commitment)", refreshed["count"])
	}
}

// TestBrainGTDTodayHonoursPinnedPathsAndFamilyFloor checks that explicit pin
// inputs and the optional family floor seed the day's list as the issue spec
// describes.
func TestBrainGTDTodayHonoursPinnedPathsAndFamilyFloor(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/filler-1.md", "Filler one", "manual", "ref-f1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/filler-2.md", "Filler two", "manual", "ref-f2")
	writeFamilyCoreCommitment(t, tmp, "work", "brain/gtd/family.md", "Family floor", "manual", "ref-fam")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/pinned.md", "Pinned outcome", "manual", "ref-pin")

	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.today", map[string]interface{}{
		"config_path":          configPath,
		"sphere":               "work",
		"date":                 "2026-05-04",
		"pinned_paths":         []interface{}{"brain/gtd/pinned.md"},
		"include_family_floor": true,
	})
	if err != nil {
		t.Fatalf("brain.gtd.today: %v", err)
	}
	items, _ := got["items"].([]gtdtoday.Item)
	if len(items) == 0 || items[0].Path != "brain/gtd/pinned.md" || !items[0].Pinned {
		t.Fatalf("first item = %+v, want pinned brain/gtd/pinned.md first", items)
	}
	familyIndex := -1
	for i, item := range items {
		if item.Path == "brain/gtd/family.md" {
			familyIndex = i
			break
		}
	}
	if familyIndex <= 0 {
		t.Fatalf("family floor item missing or out of order: %+v", items)
	}
	for i := 1; i < familyIndex; i++ {
		if !items[i].Pinned {
			t.Fatalf("non-pinned item %+v interleaved before family-core item", items[i])
		}
	}
}

func writeFamilyCoreCommitment(t *testing.T, root, sphere, rel, outcome, provider, ref string) {
	t.Helper()
	header := fmt.Sprintf("---\nkind: commitment\nsphere: %s\ntitle: %s\nstatus: next\ncontext: review\nnext_action: Review the item\noutcome: %s\nlabels:\n  - track/core\nsource_bindings:\n  - provider: %s\n    ref: %q\n---\n", sphere, outcome, outcome, provider, ref)
	body := header + fmt.Sprintf("# %s\n\n## Summary\nReview the item.\n\n## Next Action\n- [ ] Review the item\n\n## Evidence\n- %s:%s\n\n## Linked Items\n- None.\n\n## Review Notes\n- None.\n", outcome, provider, ref)
	writeMCPBrainFile(t, filepath.Join(root, sphere, filepath.FromSlash(rel)), body)
}
