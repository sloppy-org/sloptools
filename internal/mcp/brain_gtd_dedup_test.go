package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

func TestBrainGTDDedupScanReconcilesAndQueuesCandidates(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "Send alpha budget", "todoist", "t1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/c.md", "Review W7-X plots", "github", "org/repo#1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/d.md", "Review W7-X plots", "GitHub", "org/repo#1")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_scan", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.dedup_scan: %v", err)
	}
	result := got["dedup"].(braingtd.ScanResult)
	if len(result.Candidates) != 1 {
		t.Fatalf("candidate count = %d, want 1: %#v", len(result.Candidates), result.Candidates)
	}
	if aggregateWithBinding(result.Aggregates, "github:org/repo#1") == nil {
		t.Fatalf("missing reconciled GitHub aggregate: %#v", result.Aggregates)
	}
	if agg := aggregateWithBinding(result.Aggregates, "github:org/repo#1"); len(agg.Bindings) != 1 {
		t.Fatalf("duplicate binding survived: %#v", agg)
	}
}

func TestBrainGTDDedupKeepSeparateDoesNotResurface(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "send alpha budget", "todoist", "t1")

	s := NewServer(t.TempDir())
	first, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_scan", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	id := first["dedup"].(braingtd.ScanResult).Candidates[0].ID
	if _, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_review_apply",
		"config_path": configPath, "sphere": "work", "id": id, "decision": "keep_separate",
	}); err != nil {
		t.Fatalf("dedup_review_apply: %v", err)
	}
	second, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_scan", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("rescan: %v", err)
	}
	if got := len(second["dedup"].(braingtd.ScanResult).Candidates); got != 0 {
		t.Fatalf("kept-separate candidate resurfaced, count=%d", got)
	}
	history, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_history", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("history: %v", err)
	}
	if history["count"] != 2 {
		t.Fatalf("history count = %v, want 2: %#v", history["count"], history)
	}
}

func TestBrainGTDDedupMergePreservesBindings(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "send alpha budget", "todoist", "t1")

	s := NewServer(t.TempDir())
	first, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_scan", "config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	id := first["dedup"].(braingtd.ScanResult).Candidates[0].ID
	if _, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_review_apply",
		"config_path": configPath, "sphere": "work", "id": id, "decision": "merge",
		"winner_path": "brain/gtd/a.md", "outcome": "Send Alpha budget",
	}); err != nil {
		t.Fatalf("dedup_review_apply merge: %v", err)
	}
	parsed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", "config_path": configPath, "sphere": "work", "path": "brain/gtd/a.md"})
	if err != nil {
		t.Fatalf("parse winner: %v", err)
	}
	winner := parsed["commitment"].(*braingtd.Commitment)
	if len(winner.SourceBindings) != 2 || winner.Outcome != "Send Alpha budget" {
		t.Fatalf("winner = %#v", winner)
	}
	winnerData, err := os.ReadFile(filepath.Join(tmp, "work", "brain", "gtd", "a.md"))
	if err != nil {
		t.Fatalf("read winner: %v", err)
	}
	if result := braingtd.ParseAndValidate(string(winnerData)); len(result.Diagnostics) != 0 {
		t.Fatalf("winner note invalid: %#v\n%s", result.Diagnostics, string(winnerData))
	}
}

func TestBrainGTDBindCollapsesCrossSourceCommitments(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/meeting.md", "Send alpha budget", "meetings", "alpha-standup")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/mail.md", "Send alpha budget", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/todo.md", "Send alpha budget", "todoist", "t1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/github.md", "Send alpha budget", "github", "org/repo#7")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_bind",
		"config_path": configPath,
		"sphere":      "work",
		"winner_path": "brain/gtd/meeting.md",
		"paths":       []interface{}{"brain/gtd/meeting.md", "brain/gtd/mail.md", "brain/gtd/todo.md", "brain/gtd/github.md"},
		"outcome":     "Send alpha budget",
	})
	if err != nil {
		t.Fatalf("brain.gtd.bind: %v", err)
	}
	if got["binding_count"] != 4 {
		t.Fatalf("binding_count = %v, want 4: %#v", got["binding_count"], got)
	}
	parsed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", "config_path": configPath, "sphere": "work", "path": "brain/gtd/meeting.md"})
	if err != nil {
		t.Fatalf("parse winner: %v", err)
	}
	winner := parsed["commitment"].(*braingtd.Commitment)
	want := map[string]bool{"meetings:alpha-standup": false, "mail:m1": false, "todoist:t1": false, "github:org/repo#7": false}
	for _, binding := range winner.SourceBindings {
		if _, ok := want[binding.StableID()]; ok {
			want[binding.StableID()] = true
		}
	}
	for id, seen := range want {
		if !seen {
			t.Fatalf("winner missing binding %s: %#v", id, winner.SourceBindings)
		}
	}
	loser := parseDedupCommitment(t, s, configPath, "brain/gtd/github.md")
	if loser.LocalOverlay.Status != "dropped" || loser.Dedup.EquivalentTo != "brain/gtd/meeting.md" {
		t.Fatalf("loser state = %#v", loser)
	}
}

func TestBrainGTDBindAttachesNewSourceBinding(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/meeting.md", "Send alpha budget", "meetings", "alpha-standup")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_bind",
		"config_path": configPath,
		"sphere":      "work",
		"winner_path": "brain/gtd/meeting.md",
		"source_bindings": []interface{}{
			map[string]interface{}{"provider": "mail", "ref": "m1"},
		},
	})
	if err != nil {
		t.Fatalf("brain.gtd.bind attach: %v", err)
	}
	if got["binding_count"] != 2 {
		t.Fatalf("binding_count = %v, want 2: %#v", got["binding_count"], got)
	}
	winner := parseDedupCommitment(t, s, configPath, "brain/gtd/meeting.md")
	if len(winner.SourceBindings) != 2 {
		t.Fatalf("winner bindings = %#v", winner.SourceBindings)
	}
}

func TestBrainGTDDedupScanUsesLocalLLMCommand(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/a.md", "Prepare W7-X campaign slide deck", "mail", "m1")
	writeDedupCommitment(t, tmp, "work", "brain/gtd/b.md", "Draft presentation slides for W7-X campaign", "github", "org/repo#2")

	s := NewServer(t.TempDir())
	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_dedup_scan",
		"config_path":   configPath,
		"sphere":        "work",
		"llm_threshold": 0.01,
		"llm_command":   "printf '%s\n' '{\"same\":true,\"confidence\":0.92,\"reasoning\":\"same slide-deck task\"}'",
	})
	if err != nil {
		t.Fatalf("brain.gtd.dedup_scan: %v", err)
	}
	candidates := got["dedup"].(braingtd.ScanResult).Candidates
	if len(candidates) != 1 || candidates[0].Detector != "llm" || candidates[0].Reasoning == "" {
		t.Fatalf("candidates = %#v", candidates)
	}
}

func TestBrainGTDReviewListMergesMarkdownAndTodoistWithoutDuplicates(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeDedupCommitment(t, tmp, "work", "brain/gtd/todo.md", "Send alpha budget", "todoist", "work-proj/task-1")
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	start := time.Now().UTC().Add(72 * time.Hour).Truncate(time.Hour)
	deadline := start.Add(7 * 24 * time.Hour)
	provider := &fakeTasksProvider{
		name:      "todoist",
		taskLists: []providerdata.TaskList{{ID: "work-proj", Name: "Work"}},
		tasksByList: map[string][]providerdata.TaskItem{
			"work-proj": {
				{ID: "task-1", ListID: "work-proj", ProviderRef: "task-1", Title: "Send alpha budget"},
				{ID: "task-2", ListID: "work-proj", ProviderRef: "task-2", Title: "Draft beta budget", StartAt: &start, Due: &deadline},
			},
		},
	}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		if got.ID != account.ID {
			t.Fatalf("account ID = %d, want %d", got.ID, account.ID)
		}
		return provider, nil
	}

	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_review_list",
		"config_path": configPath,
		"sphere":      "work",
		"sources":     []interface{}{"markdown", "tasks"},
		"account_id":  account.ID,
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_list: %v", err)
	}
	items := got["items"].([]gtdReviewItem)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want markdown canonical item plus one new task", items)
	}
	if got["duplicate_skipped"] != 1 {
		t.Fatalf("duplicate_skipped = %#v, want 1", got["duplicate_skipped"])
	}
	task := itemByID(items, "todoist:work-proj/task-2")
	if task == nil {
		t.Fatalf("missing todoist task: %#v", items)
	}
	wantFollowUp := start.Format(time.RFC3339)
	wantDue := deadline.Format(time.RFC3339)
	if task.Queue != "deferred" || task.FollowUp != wantFollowUp || task.Due != wantDue {
		t.Fatalf("task mapping = %#v, want Queue=deferred FollowUp=%s Due=%s", task, wantFollowUp, wantDue)
	}
}

func TestBrainGTDReviewListExposesTaskHierarchyAndFilters(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	due := time.Now().UTC().Add(24 * time.Hour).Truncate(time.Hour)
	provider := &fakeTasksProvider{
		name:      "todoist",
		taskLists: []providerdata.TaskList{{ID: "work-proj", Name: "Work"}},
		tasksByList: map[string][]providerdata.TaskItem{
			"work-proj": {
				{ID: "parent", ListID: "work-proj", ProviderRef: "parent", Title: "Publish manual"},
				{ID: "child", ListID: "work-proj", ProviderRef: "child", ParentID: "parent", Title: "Check references", Due: &due},
			},
		},
	}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		if got.ID != account.ID {
			t.Fatalf("account ID = %d, want %d", got.ID, account.ID)
		}
		return provider, nil
	}

	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_review_list",
		"sphere":     "work",
		"sources":    []interface{}{"tasks"},
		"account_id": account.ID,
		"queue":      "next",
		"project":    "Work",
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_list: %v", err)
	}
	items := got["items"].([]gtdReviewItem)
	parent := itemByID(items, "todoist:work-proj/parent")
	child := itemByID(items, "todoist:work-proj/child")
	if parent == nil || parent.Kind != "project" {
		t.Fatalf("parent item = %#v, want kind=project in %#v", parent, items)
	}
	if child == nil || child.ParentID != "parent" {
		t.Fatalf("child item = %#v, want parent_id=parent in %#v", child, items)
	}

	filtered, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_review_list",
		"sphere":     "work",
		"sources":    []interface{}{"tasks"},
		"account_id": account.ID,
		"due_before": due.Add(time.Hour).Format(time.RFC3339),
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_list filtered: %v", err)
	}
	if gotItems := filtered["items"].([]gtdReviewItem); len(gotItems) != 1 || gotItems[0].ID != "todoist:work-proj/child" {
		t.Fatalf("filtered items = %#v, want only due child", gotItems)
	}
}

func TestBrainGTDReviewListUsesBulkTaskListingWhenAvailable(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeTasksProvider{
		name:      store.ExternalProviderTodoist,
		taskLists: []providerdata.TaskList{{ID: "inbox", Name: "Inbox"}, {ID: "research", Name: "Research"}},
		tasksByList: map[string][]providerdata.TaskItem{
			"inbox":    {{ID: "should-not-run", ListID: "inbox", ProviderRef: "should-not-run", Title: "per-list fetch should stay unused"}},
			"research": {{ID: "should-not-run-2", ListID: "research", ProviderRef: "should-not-run-2", Title: "per-list fetch should stay unused"}},
		},
		allTasks: []providerdata.TaskItem{
			{ID: "task-1", ListID: "inbox", ProviderRef: "task-1", Title: "Inbox item"},
			{ID: "task-2", ListID: "research", ProviderRef: "task-2", Title: "Research item"},
		},
	}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		if got.ID != account.ID {
			t.Fatalf("account ID = %d, want %d", got.ID, account.ID)
		}
		return provider, nil
	}

	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_review_list",
		"sphere":     "work",
		"sources":    []interface{}{"tasks"},
		"account_id": account.ID,
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_list: %v", err)
	}
	items := got["items"].([]gtdReviewItem)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want 2", items)
	}
	if provider.listAllCalls != 1 {
		t.Fatalf("listAllCalls = %d, want 1", provider.listAllCalls)
	}
	if provider.listCalls != 1 {
		t.Fatalf("listCalls = %d, want 1 ListTaskLists call only", provider.listCalls)
	}
	if itemByID(items, "todoist:inbox/task-1") == nil || itemByID(items, "todoist:research/task-2") == nil {
		t.Fatalf("bulk-listed items missing from %#v", items)
	}
}

func TestBrainGTDReviewListAggregatesMultipleTaskBackendsInSphere(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	exchange, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "Exchange", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(exchange): %v", err)
	}
	todoistAccount, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderTodoist, "Todoist", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount(todoist): %v", err)
	}
	exchangeProvider := &fakeTasksProvider{
		name:      store.ExternalProviderExchangeEWS,
		taskLists: []providerdata.TaskList{{ID: "exchange-list", Name: "Exchange"}},
		tasksByList: map[string][]providerdata.TaskItem{
			"exchange-list": {{ID: "ews-1", ListID: "exchange-list", ProviderRef: "ews-1", Title: "Reply to travel office"}},
		},
	}
	todoistProvider := &fakeTasksProvider{
		name:      store.ExternalProviderTodoist,
		taskLists: []providerdata.TaskList{{ID: "todo-project", Name: "Todoist"}},
		tasksByList: map[string][]providerdata.TaskItem{
			"todo-project": {{ID: "task-1", ListID: "todo-project", ProviderRef: "task-1", Title: "Draft beta budget"}},
		},
	}
	s.newTasksProvider = func(_ context.Context, got store.ExternalAccount) (tasks.Provider, error) {
		switch got.ID {
		case exchange.ID:
			return exchangeProvider, nil
		case todoistAccount.ID:
			return todoistProvider, nil
		default:
			t.Fatalf("unexpected account ID %d", got.ID)
			return nil, nil
		}
	}

	got, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "gtd_review_list",
		"sphere":  "work",
		"sources": []interface{}{"tasks"},
	})
	if err != nil {
		t.Fatalf("brain.gtd.review_list: %v", err)
	}
	items := got["items"].([]gtdReviewItem)
	if len(items) != 2 {
		t.Fatalf("items = %#v, want one item per tasks-capable backend", items)
	}
	if itemByID(items, "exchange_ews:exchange-list/ews-1") == nil {
		t.Fatalf("missing exchange item in %#v", items)
	}
	if itemByID(items, "todoist:todo-project/task-1") == nil {
		t.Fatalf("missing todoist item in %#v", items)
	}
}

func TestGTDReviewItemFromSourceItemMapsIssueState(t *testing.T) {
	item := gtdReviewItemFromSourceItem(providerdata.SourceItem{
		Provider:  "github",
		Kind:      "issue",
		Container: "sloppy-org/sloptools",
		Number:    43,
		Title:     "Add GTD tools",
		State:     "OPEN",
		SourceRef: "github:sloppy-org/sloptools#43",
	})
	if item.ID != "github:sloppy-org/sloptools#43" || item.Queue != "next" {
		t.Fatalf("open issue item = %#v", item)
	}
	item = gtdReviewItemFromSourceItem(providerdata.SourceItem{
		Provider:  "gitlab",
		Kind:      "merge_request",
		Container: "plasma/repo",
		Number:    7,
		Title:     "Closed MR",
		State:     "merged",
		SourceRef: "gitlab:plasma/repo!7",
	})
	if item.ID != "gitlab:plasma/repo!7" || item.Queue != "done" {
		t.Fatalf("merged MR item = %#v", item)
	}
}

func parseDedupCommitment(t *testing.T, s *Server, configPath, path string) *braingtd.Commitment {
	t.Helper()
	parsed, err := s.callTool("sloppy_brain", map[string]interface{}{"action": "note_parse", "config_path": configPath, "sphere": "work", "path": path})
	if err != nil {
		t.Fatalf("parse %s: %v", path, err)
	}
	return parsed["commitment"].(*braingtd.Commitment)
}

func writeDedupCommitment(t *testing.T, root, sphere, rel, outcome, provider, ref string) {
	t.Helper()
	header := fmt.Sprintf("---\nkind: commitment\nsphere: %s\ntitle: %s\nstatus: next\ncontext: review\nnext_action: Review the item\noutcome: %s\nsource_bindings:\n  - provider: %s\n    ref: %q\n---\n", sphere, outcome, outcome, provider, ref)
	body := header + fmt.Sprintf("# %s\n\n## Summary\nReview the item.\n\n## Next Action\n- [ ] Review the item\n\n## Evidence\n- %s:%s\n\n## Linked Items\n- None.\n\n## Review Notes\n- None.\n", outcome, provider, ref)
	writeMCPBrainFile(t, filepath.Join(root, sphere, filepath.FromSlash(rel)), body)
}

func aggregateWithBinding(aggregates []braingtd.Aggregate, id string) *braingtd.Aggregate {
	for i := range aggregates {
		for _, bindingID := range aggregates[i].BindingIDs {
			if bindingID == id {
				return &aggregates[i]
			}
		}
	}
	return nil
}

func itemByID(items []gtdReviewItem, id string) *gtdReviewItem {
	for i := range items {
		if items[i].ID == id {
			return &items[i]
		}
	}
	return nil
}
