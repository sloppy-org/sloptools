package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/sourceitems"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

type gtdReviewItem struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	SourceRef    string   `json:"source_ref,omitempty"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Queue        string   `json:"queue"`
	Kind         string   `json:"kind,omitempty"`
	URL          string   `json:"url,omitempty"`
	Path         string   `json:"path,omitempty"`
	Due          string   `json:"due,omitempty"`
	FollowUp     string   `json:"follow_up,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Actor        string   `json:"actor,omitempty"`
	Project      string   `json:"project,omitempty"`
	ExistingPath string   `json:"existing_path,omitempty"`
}

type gtdReviewBuild struct {
	items          []gtdReviewItem
	bindings       map[string]string
	seen           map[string]struct{}
	errors         []string
	duplicateCount int
}

func (s *Server) brainGTDReviewList(args map[string]interface{}) (map[string]interface{}, error) {
	build := gtdReviewBuild{bindings: make(map[string]string), seen: make(map[string]struct{})}
	sources := gtdReviewSources(args)
	if sources["markdown"] {
		if err := s.addMarkdownGTDItems(args, &build); err != nil {
			return nil, err
		}
	}
	if sources["tasks"] || sources["todoist"] {
		s.addTaskGTDItems(args, &build)
	}
	if sources["source"] || sources["sources"] || sources["issues"] {
		s.addIssueGTDItems(args, &build)
	}
	sort.SliceStable(build.items, func(i, j int) bool {
		if build.items[i].Queue != build.items[j].Queue {
			return gtdQueueRank(build.items[i].Queue) < gtdQueueRank(build.items[j].Queue)
		}
		if build.items[i].Due != build.items[j].Due {
			return build.items[i].Due < build.items[j].Due
		}
		return strings.ToLower(build.items[i].Title) < strings.ToLower(build.items[j].Title)
	})
	limit := intArg(args, "limit", 0)
	if limit > 0 && len(build.items) > limit {
		build.items = build.items[:limit]
	}
	return map[string]interface{}{
		"sphere":             strArg(args, "sphere"),
		"items":              build.items,
		"count":              len(build.items),
		"duplicate_skipped":  build.duplicateCount,
		"errors":             build.errors,
		"markdown_canonical": true,
	}, nil
}

func gtdReviewSources(args map[string]interface{}) map[string]bool {
	values := stringListArg(args, "sources")
	if len(values) == 0 {
		values = []string{"markdown", "tasks", "source"}
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out
}

func (s *Server) addMarkdownGTDItems(args map[string]interface{}, build *gtdReviewBuild) error {
	notes, _, err := s.loadDedupNotes(args)
	if err != nil {
		return err
	}
	for _, note := range notes {
		item := gtdReviewItemFromCommitment(note)
		for _, binding := range note.Entry.Commitment.SourceBindings {
			id := binding.StableID()
			if id != "" {
				build.bindings[id] = note.Entry.Path
			}
		}
		build.add(item)
	}
	return nil
}

func (s *Server) addTaskGTDItems(args map[string]interface{}, build *gtdReviewBuild) {
	account, provider, err := s.tasksProviderForTool(args)
	if err != nil {
		build.errors = append(build.errors, err.Error())
		return
	}
	defer provider.Close()
	lists, err := taskListsForReview(context.Background(), provider, stringListArg(args, "list_ids"))
	if err != nil {
		build.errors = append(build.errors, err.Error())
		return
	}
	for _, list := range lists {
		items, err := provider.ListTasks(context.Background(), list.ID)
		if err != nil {
			build.errors = append(build.errors, err.Error())
			continue
		}
		for _, item := range items {
			build.addOrSkipExisting(gtdReviewItemFromTask(account.Sphere, provider.ProviderName(), list, item))
		}
	}
}

func taskListsForReview(ctx context.Context, provider tasks.Provider, ids []string) ([]providerdata.TaskList, error) {
	if len(ids) == 0 {
		return provider.ListTaskLists(ctx)
	}
	available, err := provider.ListTaskLists(ctx)
	if err != nil {
		return nil, err
	}
	byID := make(map[string]providerdata.TaskList, len(available))
	for _, list := range available {
		byID[strings.TrimSpace(list.ID)] = list
	}
	out := make([]providerdata.TaskList, 0, len(ids))
	for _, id := range ids {
		list, ok := byID[strings.TrimSpace(id)]
		if !ok {
			return nil, fmt.Errorf("task list %q not found", id)
		}
		out = append(out, list)
	}
	return out, nil
}

func (s *Server) addIssueGTDItems(args map[string]interface{}, build *gtdReviewBuild) {
	for _, dir := range stringListArg(args, "project_dirs") {
		provider, err := sourceProviderForReview(dir, strArg(args, "provider"))
		if err != nil {
			build.errors = append(build.errors, err.Error())
			continue
		}
		items, err := provider.List(context.Background())
		if err != nil {
			build.errors = append(build.errors, err.Error())
			continue
		}
		for _, item := range items {
			build.addOrSkipExisting(gtdReviewItemFromSourceItem(item))
		}
	}
}

func sourceProviderForReview(projectDir, providerName string) (sourceitems.Provider, error) {
	if strings.TrimSpace(projectDir) == "" {
		projectDir = "."
	}
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "", "auto":
		detected, err := sourceitems.DetectProvider(projectDir)
		if err != nil {
			return nil, err
		}
		return sourceProviderForReview(projectDir, detected)
	case sourceitems.GitHubProviderName:
		return sourceitems.NewGitHubProvider(projectDir)
	case sourceitems.GitLabProviderName:
		return sourceitems.NewGitLabProvider(projectDir)
	default:
		return nil, fmt.Errorf("unsupported source provider %q", providerName)
	}
}

func (b *gtdReviewBuild) addOrSkipExisting(item gtdReviewItem) {
	if path := b.bindings[item.ID]; path != "" {
		b.duplicateCount++
		return
	}
	b.add(item)
}

func (b *gtdReviewBuild) add(item gtdReviewItem) {
	if strings.TrimSpace(item.ID) == "" {
		item.ID = item.Source + ":" + item.Title
	}
	if _, ok := b.seen[item.ID]; ok {
		b.duplicateCount++
		return
	}
	b.seen[item.ID] = struct{}{}
	b.items = append(b.items, item)
}

func gtdReviewItemFromCommitment(note dedupNote) gtdReviewItem {
	c := note.Entry.Commitment
	status := effectiveGTDStatus(c)
	return gtdReviewItem{
		ID:       "markdown:" + note.Entry.Path,
		Source:   "markdown",
		Title:    firstNonEmpty(c.Outcome, c.Title, c.NextAction, filepath.Base(note.Entry.Path)),
		Status:   status,
		Queue:    gtdQueue(status, c.FollowUp, time.Now().UTC()),
		Path:     note.Entry.Path,
		Due:      c.Due,
		FollowUp: c.FollowUp,
		Labels:   append([]string(nil), c.Labels...),
		Actor:    firstNonEmpty(c.WaitingFor, c.Actor),
		Project:  c.Project,
	}
}

func gtdReviewItemFromTask(sphere, providerName string, list providerdata.TaskList, task providerdata.TaskItem) gtdReviewItem {
	binding := braingtd.SourceBinding{Provider: providerName, Ref: taskBindingRef(list.ID, task)}
	status := taskGTDStatus(list, task)
	return gtdReviewItem{
		ID:        binding.StableID(),
		Source:    providerName,
		SourceRef: binding.Ref,
		Title:     task.Title,
		Status:    status,
		Queue:     gtdQueue(status, timeString(task.StartAt), time.Now().UTC()),
		Kind:      "task",
		URL:       task.ProviderURL,
		Due:       timeString(task.Due),
		FollowUp:  timeString(task.StartAt),
		Labels:    append([]string(nil), task.Labels...),
		Actor:     firstNonEmpty(task.AssigneeName, task.AssigneeID),
		Project:   firstNonEmpty(list.Name, task.ProjectID, sphere),
	}
}

func gtdReviewItemFromSourceItem(source providerdata.SourceItem) gtdReviewItem {
	binding := braingtd.SourceBinding{Provider: source.Provider, Ref: strings.TrimPrefix(source.SourceRef, source.Provider+":"), URL: source.URL}
	status := "next"
	if sourceClosed(source.State) {
		status = "done"
	}
	return gtdReviewItem{
		ID:        binding.StableID(),
		Source:    source.Provider,
		SourceRef: binding.Ref,
		Title:     source.Title,
		Status:    status,
		Queue:     gtdQueue(status, "", time.Now().UTC()),
		Kind:      source.Kind,
		URL:       source.URL,
		Labels:    append([]string(nil), source.Labels...),
		Actor:     firstNonEmpty(strings.Join(source.Assignees, ", "), source.Author),
		Project:   source.Container,
	}
}

func taskBindingRef(listID string, task providerdata.TaskItem) string {
	id := strings.TrimSpace(task.ProviderRef)
	if id == "" {
		id = strings.TrimSpace(task.ID)
	}
	list := strings.TrimSpace(firstNonEmpty(task.ListID, listID))
	if list == "" {
		return id
	}
	return list + "/" + id
}

func taskGTDStatus(list providerdata.TaskList, task providerdata.TaskItem) string {
	if task.Completed {
		return "done"
	}
	for _, label := range task.Labels {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "waiting", "waiting-for", "waiting_for":
			return "waiting"
		case "someday", "maybe", "someday-maybe", "someday_maybe":
			return "someday"
		case "maybe_stale", "needs-review", "needs_review":
			return "maybe_stale"
		}
	}
	if task.StartAt != nil && task.StartAt.After(time.Now().UTC()) {
		return "deferred"
	}
	if list.Primary || list.IsInboxProject {
		return "inbox"
	}
	return "next"
}

func effectiveGTDStatus(c braingtd.Commitment) string {
	if strings.TrimSpace(c.LocalOverlay.Status) != "" {
		return strings.ToLower(strings.TrimSpace(c.LocalOverlay.Status))
	}
	return strings.ToLower(strings.TrimSpace(c.Status))
}

func gtdQueue(status, followUp string, now time.Time) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", "done", "dropped":
		return "done"
	case "waiting":
		return "waiting"
	case "deferred":
		if readyAt(followUp, now) {
			return "next"
		}
		return "deferred"
	case "someday":
		return "someday"
	case "maybe_stale", "needs_review":
		return "review"
	case "next":
		return "next"
	default:
		return "inbox"
	}
}

func readyAt(raw string, now time.Time) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	if t := parseRFC3339OrDate(raw); !t.IsZero() {
		return !t.After(now)
	}
	return false
}

func timeString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func sourceClosed(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "closed", "merged", "done":
		return true
	default:
		return false
	}
}

func gtdQueueRank(queue string) int {
	switch queue {
	case "inbox":
		return 0
	case "next":
		return 1
	case "waiting":
		return 2
	case "deferred":
		return 3
	case "review":
		return 4
	case "someday":
		return 5
	case "done":
		return 6
	default:
		return 7
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}
