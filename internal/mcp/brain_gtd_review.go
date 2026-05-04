package mcp

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/mcp/gtdfocus"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/sourceitems"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
	"github.com/sloppy-org/sloptools/pkg/taskgtd"
)

func (s *Server) brainGTDTracks(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	return gtdfocus.Tracks(cfg, strings.TrimSpace(strArg(args, "sphere")))
}

func (s *Server) brainGTDFocus(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	return gtdfocus.Focus(st, strings.TrimSpace(strArg(args, "sphere")), args)
}

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
	Track        string   `json:"track,omitempty"`
	ParentID     string   `json:"parent_id,omitempty"`
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
	build.items = filterGTDReviewItems(build.items, args)
	sort.SliceStable(build.items, func(i, j int) bool {
		if build.items[i].Queue != build.items[j].Queue {
			return taskgtd.QueueRank(build.items[i].Queue) < taskgtd.QueueRank(build.items[j].Queue)
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
	accounts, err := s.taskAccountsForReview(args)
	if err != nil {
		build.errors = append(build.errors, err.Error())
		return
	}
	ctx := context.Background()
	listIDs := stringListArg(args, "list_ids")
	for _, account := range accounts {
		provider, err := s.tasksProviderForAccount(ctx, account)
		if err != nil {
			build.errors = append(build.errors, fmt.Sprintf("%s %q: %v", account.Provider, account.AccountName, err))
			continue
		}
		func() {
			defer provider.Close()
			lists, err := taskListsForReview(ctx, provider, listIDs)
			if err != nil {
				build.errors = append(build.errors, fmt.Sprintf("%s %q: %v", account.Provider, account.AccountName, err))
				return
			}
			if len(listIDs) == 0 {
				if bulk, ok := provider.(tasks.BulkLister); ok {
					items, err := bulk.ListAllTasks(ctx)
					if err == nil {
						addBulkTaskReviewItems(build, account.Sphere, provider.ProviderName(), lists, items)
						return
					}
					if !errors.Is(err, tasks.ErrUnsupported) {
						build.errors = append(build.errors, fmt.Sprintf("%s %q: %v", account.Provider, account.AccountName, err))
						return
					}
				}
			}
			for _, list := range lists {
				items, err := provider.ListTasks(ctx, list.ID)
				if err != nil {
					build.errors = append(build.errors, fmt.Sprintf("%s %q list %q: %v", account.Provider, account.AccountName, list.Name, err))
					continue
				}
				parentIDs := taskgtd.ParentTaskIDs(taskGTDTasks(items))
				for _, item := range items {
					reviewItem := gtdReviewItemFromTask(account.Sphere, provider.ProviderName(), list, item)
					if parentIDs[strings.TrimSpace(item.ID)] {
						reviewItem.Kind = "project"
					}
					build.addOrSkipExisting(reviewItem)
				}
			}
		}()
	}
}

func addBulkTaskReviewItems(build *gtdReviewBuild, sphere, providerName string, lists []providerdata.TaskList, items []providerdata.TaskItem) {
	byID := make(map[string]providerdata.TaskList, len(lists))
	for _, list := range lists {
		byID[strings.TrimSpace(list.ID)] = list
	}
	parentIDs := taskgtd.ParentTaskIDs(taskGTDTasks(items))
	for _, item := range items {
		list := taskListForReviewItem(byID, item)
		reviewItem := gtdReviewItemFromTask(sphere, providerName, list, item)
		if parentIDs[strings.TrimSpace(item.ID)] {
			reviewItem.Kind = "project"
		}
		build.addOrSkipExisting(reviewItem)
	}
}

func taskListForReviewItem(byID map[string]providerdata.TaskList, item providerdata.TaskItem) providerdata.TaskList {
	for _, candidate := range []string{strings.TrimSpace(item.ListID), strings.TrimSpace(item.ProjectID)} {
		if candidate == "" {
			continue
		}
		if list, ok := byID[candidate]; ok {
			return list
		}
		return providerdata.TaskList{ID: candidate, Name: candidate}
	}
	return providerdata.TaskList{}
}

func (s *Server) taskAccountsForReview(args map[string]interface{}) ([]store.ExternalAccount, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	accountIDPtr, _, err := optionalInt64Arg(args, "account_id")
	if err != nil {
		return nil, err
	}
	if accountIDPtr != nil {
		account, err := accountForTool(st, args, "tasks-capable", isTasksCapableProvider)
		if err != nil {
			return nil, err
		}
		return []store.ExternalAccount{account}, nil
	}
	accounts, err := st.ListExternalAccounts(strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		return nil, err
	}
	matches := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled || !isTasksCapableProvider(account.Provider) {
			continue
		}
		matches = append(matches, account)
	}
	if len(matches) == 0 {
		sphere := strings.TrimSpace(strArg(args, "sphere"))
		if sphere != "" {
			return nil, fmt.Errorf("no enabled tasks-capable account for sphere %q", sphere)
		}
		return nil, fmt.Errorf("no enabled tasks-capable account is configured")
	}
	if len(stringListArg(args, "list_ids")) > 0 && len(matches) > 1 {
		return nil, errors.New("account_id is required when list_ids are supplied and multiple tasks-capable accounts are configured")
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Sphere != matches[j].Sphere {
			return matches[i].Sphere < matches[j].Sphere
		}
		if matches[i].Provider != matches[j].Provider {
			return matches[i].Provider < matches[j].Provider
		}
		return matches[i].ID < matches[j].ID
	})
	return matches, nil
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
		ID: "markdown:" + note.Entry.Path, Source: "markdown",
		Title:  firstNonEmpty(c.Outcome, c.Title, c.NextAction, filepath.Base(note.Entry.Path)),
		Status: status, Queue: taskgtd.Queue(status, c.FollowUp, time.Now().UTC()),
		Path:    note.Entry.Path,
		Due:     c.Due,
		Labels:  append([]string(nil), c.Labels...),
		Actor:   firstNonEmpty(c.WaitingFor, c.Actor),
		Project: c.Project,
		Track:   c.EffectiveTrack(), FollowUp: c.FollowUp,
	}
}

func gtdReviewItemFromTask(sphere, providerName string, list providerdata.TaskList, task providerdata.TaskItem) gtdReviewItem {
	modelList := taskGTDList(list)
	modelTask := taskGTDTask(task)
	binding := braingtd.SourceBinding{Provider: providerName, Ref: taskgtd.BindingRef(list.ID, modelTask)}
	status := taskgtd.Status(modelList, modelTask, time.Now().UTC())
	return gtdReviewItem{
		ID: binding.StableID(), Source: providerName, SourceRef: binding.Ref,
		Title: task.Title, Status: status,
		Queue:    taskgtd.Queue(status, taskgtd.TimeString(task.StartAt), time.Now().UTC()),
		Kind:     "task",
		URL:      task.ProviderURL,
		Due:      taskgtd.TimeString(task.Due),
		FollowUp: taskgtd.TimeString(task.StartAt),
		Labels:   append([]string(nil), task.Labels...),
		Actor:    firstNonEmpty(task.AssigneeName, task.AssigneeID),
		Project:  firstNonEmpty(list.Name, task.ProjectID, sphere),
		Track:    braingtd.TrackFromLabels(task.Labels),
		ParentID: strings.TrimSpace(task.ParentID),
	}
}

func taskGTDList(list providerdata.TaskList) taskgtd.List {
	return taskgtd.List{ID: list.ID, Name: list.Name, Primary: list.Primary, IsInboxProject: list.IsInboxProject}
}

func taskGTDTasks(tasks []providerdata.TaskItem) []taskgtd.Task {
	out := make([]taskgtd.Task, 0, len(tasks))
	for _, task := range tasks {
		out = append(out, taskGTDTask(task))
	}
	return out
}

func taskGTDTask(task providerdata.TaskItem) taskgtd.Task {
	return taskgtd.Task{
		ID: task.ID, ListID: task.ListID, Title: task.Title, ProjectID: task.ProjectID,
		ParentID: task.ParentID, ProviderRef: task.ProviderRef,
		Labels:    append([]string(nil), task.Labels...),
		StartAt:   task.StartAt, Due: task.Due, Completed: task.Completed,
		AssigneeID: task.AssigneeID, AssigneeName: task.AssigneeName, ProviderURL: task.ProviderURL,
	}
}

func filterGTDReviewItems(items []gtdReviewItem, args map[string]interface{}) []gtdReviewItem {
	if len(items) == 0 {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if gtdReviewItemMatches(item, args) {
			out = append(out, item)
		}
	}
	return out
}

func gtdReviewItemMatches(item gtdReviewItem, args map[string]interface{}) bool {
	if queue := strings.TrimSpace(strArg(args, "queue")); queue != "" && !strings.EqualFold(item.Queue, queue) {
		return false
	}
	if project := strings.TrimSpace(strArg(args, "project")); project != "" && !strings.EqualFold(item.Project, project) {
		return false
	}
	if track := strings.TrimSpace(strArg(args, "track")); track != "" && !strings.EqualFold(item.Track, track) {
		return false
	}
	return reviewTimeMatches(item.Due, args, "due_after", false) &&
		reviewTimeMatches(item.Due, args, "due_before", true) &&
		reviewTimeMatches(item.FollowUp, args, "follow_up_after", false) &&
		reviewTimeMatches(item.FollowUp, args, "follow_up_before", true)
}

func reviewTimeMatches(value string, args map[string]interface{}, key string, before bool) bool {
	boundText := strings.TrimSpace(strArg(args, key))
	if boundText == "" {
		return true
	}
	bound := parseRFC3339OrDate(boundText)
	if bound.IsZero() {
		return false
	}
	t := parseRFC3339OrDate(value)
	if t.IsZero() {
		return false
	}
	if before {
		return t.Before(bound) || t.Equal(bound)
	}
	return t.After(bound) || t.Equal(bound)
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
		Queue:     taskgtd.Queue(status, "", time.Now().UTC()),
		Kind:      source.Kind,
		URL:       source.URL,
		Labels:    append([]string(nil), source.Labels...),
		Actor:     firstNonEmpty(strings.Join(source.Assignees, ", "), source.Author),
		Project:   source.Container,
		Track:     braingtd.TrackFromLabels(source.Labels),
	}
}

func effectiveGTDStatus(c braingtd.Commitment) string {
	if strings.TrimSpace(c.LocalOverlay.Status) != "" {
		return strings.ToLower(strings.TrimSpace(c.LocalOverlay.Status))
	}
	return strings.ToLower(strings.TrimSpace(c.Status))
}

func sourceClosed(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "closed", "merged", "done":
		return true
	default:
		return false
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
