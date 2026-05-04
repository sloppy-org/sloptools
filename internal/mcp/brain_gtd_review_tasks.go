package mcp

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/brain/gtd/focus"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
	"github.com/sloppy-org/sloptools/pkg/taskgtd"
)

// queueCounts buckets the rendered review items by their queue. The map is
// stable across calls so callers (slopshell, dashboards) can count `delegated`
// separately from `waiting` per the Manager's-Path delegation rule.
func queueCounts(items []gtdReviewItem) map[string]int {
	counts := make(map[string]int, 8)
	for _, item := range items {
		queue := strings.TrimSpace(item.Queue)
		if queue == "" {
			queue = taskgtd.StatusInbox
		}
		counts[queue]++
	}
	return counts
}

// loadGTDTracksConfig resolves the gtd.toml path from the gtd_config arg
// or the default $HOME/.config/sloptools/gtd.toml location and returns the
// parsed config. A missing default file is not an error; tools degrade to
// the unconfigured signal.
func (s *Server) loadGTDTracksConfig(args map[string]interface{}) (*gtdfocus.TracksConfig, error) {
	resolved, _, err := sloptoolsConfigPath(strArg(args, "gtd_config"), "gtd.toml")
	if err != nil {
		return nil, err
	}
	return gtdfocus.LoadTracksConfig(resolved)
}

// overWIPTracks returns the alphabetical list of track names whose open
// active count exceeds the configured wip_limit for sphere. Active items
// are those whose effective queue is "next" or "in_progress". Empty when
// no configured track is over its limit. The signal is informational; no
// caller filters items based on it.
func overWIPTracks(items []gtdReviewItem, tracksCfg *gtdfocus.TracksConfig, sphere string) []string {
	configured := tracksCfg.SphereTracks(sphere)
	if len(configured) == 0 {
		return []string{}
	}
	counts := map[string]int{}
	for _, item := range items {
		if item.Queue != taskgtd.StatusNext && item.Queue != taskgtd.StatusInProgress {
			continue
		}
		track := strings.TrimSpace(item.Track)
		if track == "" {
			continue
		}
		counts[strings.ToLower(track)]++
	}
	out := make([]string, 0, len(configured))
	for _, track := range configured {
		if track.WIPLimit <= 0 {
			continue
		}
		if counts[track.Name] > track.WIPLimit {
			out = append(out, track.Name)
		}
	}
	sort.Strings(out)
	return out
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
		Labels:  append([]string(nil), task.Labels...),
		StartAt: task.StartAt, Due: task.Due, Completed: task.Completed,
		AssigneeID: task.AssigneeID, AssigneeName: task.AssigneeName, ProviderURL: task.ProviderURL,
	}
}
