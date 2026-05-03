package taskgtd

import (
	"strings"
	"time"
)

const (
	StatusInbox      = "inbox"
	StatusNext       = "next"
	StatusWaiting    = "waiting"
	StatusDeferred   = "deferred"
	StatusSomeday    = "someday"
	StatusDone       = "done"
	StatusReview     = "review"
	StatusMaybeStale = "maybe_stale"
)

type List struct {
	ID             string
	Name           string
	Primary        bool
	IsInboxProject bool
}

type Task struct {
	ID           string
	ListID       string
	Title        string
	ProjectID    string
	ParentID     string
	ProviderRef  string
	Labels       []string
	StartAt      *time.Time
	Due          *time.Time
	Completed    bool
	AssigneeID   string
	AssigneeName string
	ProviderURL  string
}

func ParentTaskIDs(tasks []Task) map[string]bool {
	out := map[string]bool{}
	for _, task := range tasks {
		if parentID := strings.TrimSpace(task.ParentID); parentID != "" {
			out[parentID] = true
		}
	}
	return out
}

func BindingRef(listID string, task Task) string {
	id := strings.TrimSpace(task.ProviderRef)
	if id == "" {
		id = strings.TrimSpace(task.ID)
	}
	list := strings.TrimSpace(task.ListID)
	if list == "" {
		list = strings.TrimSpace(listID)
	}
	if list == "" {
		return id
	}
	return list + "/" + id
}

func Status(list List, task Task, now time.Time) string {
	if task.Completed {
		return StatusDone
	}
	for _, label := range task.Labels {
		switch strings.ToLower(strings.TrimSpace(label)) {
		case "waiting", "waiting-for", "waiting_for":
			return StatusWaiting
		case "someday", "maybe", "someday-maybe", "someday_maybe":
			return StatusSomeday
		case "maybe_stale", "needs-review", "needs_review":
			return StatusMaybeStale
		}
	}
	if task.StartAt != nil && task.StartAt.After(now.UTC()) {
		return StatusDeferred
	}
	if list.Primary || list.IsInboxProject {
		return StatusInbox
	}
	return StatusNext
}

func Queue(status, followUp string, now time.Time) string {
	switch strings.ToLower(strings.TrimSpace(status)) {
	case "closed", StatusDone, "dropped":
		return StatusDone
	case StatusWaiting:
		return StatusWaiting
	case StatusDeferred:
		if ReadyAt(followUp, now) {
			return StatusNext
		}
		return StatusDeferred
	case StatusSomeday:
		return StatusSomeday
	case StatusMaybeStale, "needs_review":
		return StatusReview
	case StatusNext:
		return StatusNext
	default:
		return StatusInbox
	}
}

func ReadyAt(raw string, now time.Time) bool {
	if strings.TrimSpace(raw) == "" {
		return false
	}
	if t := parseTimeOrDate(raw); !t.IsZero() {
		return !t.After(now.UTC())
	}
	return false
}

func QueueRank(queue string) int {
	switch queue {
	case StatusInbox:
		return 0
	case StatusNext:
		return 1
	case StatusWaiting:
		return 2
	case StatusDeferred:
		return 3
	case StatusReview:
		return 4
	case StatusSomeday:
		return 5
	case StatusDone:
		return 6
	default:
		return 7
	}
}

func TimeString(value *time.Time) string {
	if value == nil || value.IsZero() {
		return ""
	}
	return value.UTC().Format(time.RFC3339)
}

func parseTimeOrDate(raw string) time.Time {
	text := strings.TrimSpace(raw)
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02"} {
		if parsed, err := time.Parse(layout, text); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}
