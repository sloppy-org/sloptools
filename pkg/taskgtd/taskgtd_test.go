package taskgtd

import (
	"reflect"
	"testing"
	"time"
)

func TestParentTaskIDs(t *testing.T) {
	got := ParentTaskIDs([]Task{
		{ID: "parent"},
		{ID: "child-a", ParentID: "parent"},
		{ID: "child-b", ParentID: " parent "},
		{ID: "orphan"},
	})
	want := map[string]bool{"parent": true}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("ParentTaskIDs() = %#v, want %#v", got, want)
	}
}

func TestStatusAndQueue(t *testing.T) {
	now := time.Date(2026, 5, 3, 12, 0, 0, 0, time.UTC)
	future := now.Add(24 * time.Hour)
	past := now.Add(-24 * time.Hour)
	if got := Status(List{}, Task{StartAt: &future}, now); got != StatusDeferred {
		t.Fatalf("future status = %q, want deferred", got)
	}
	if got := Status(List{}, Task{StartAt: &past}, now); got != StatusNext {
		t.Fatalf("past start status = %q, want next", got)
	}
	if got := Status(List{Primary: true}, Task{StartAt: &past}, now); got != StatusInbox {
		t.Fatalf("past start primary status = %q, want inbox", got)
	}
	if got := Status(List{}, Task{StartAt: &now}, now); got != StatusNext {
		t.Fatalf("equal-now start status = %q, want next", got)
	}
	if got := Queue(StatusDeferred, TimeString(&future), now); got != StatusDeferred {
		t.Fatalf("future queue = %q, want deferred", got)
	}
	if got := Queue(StatusDeferred, TimeString(&past), now); got != StatusNext {
		t.Fatalf("ready deferred queue = %q, want next", got)
	}
	if got := Status(List{Primary: true}, Task{}, now); got != StatusInbox {
		t.Fatalf("primary list status = %q, want inbox", got)
	}
	if got := Status(List{}, Task{Labels: []string{"waiting-for"}}, now); got != StatusWaiting {
		t.Fatalf("waiting label status = %q, want waiting", got)
	}
	if got := Status(List{}, Task{Labels: []string{"delegated"}}, now); got != StatusDelegated {
		t.Fatalf("delegated label status = %q, want delegated", got)
	}
	if got := Queue(StatusDelegated, "", now); got != StatusDelegated {
		t.Fatalf("delegated queue = %q, want delegated", got)
	}
	if got := Queue("delegated_to", "", now); got != StatusDelegated {
		t.Fatalf("delegated_to alias queue = %q, want delegated", got)
	}
	if got := QueueRank(StatusDelegated); got <= QueueRank(StatusWaiting) || got >= QueueRank(StatusDeferred) {
		t.Fatalf("delegated rank %d should sort between waiting %d and deferred %d", got, QueueRank(StatusWaiting), QueueRank(StatusDeferred))
	}
	if got := Queue(StatusInProgress, "", now); got != StatusInProgress {
		t.Fatalf("in_progress queue = %q, want in_progress", got)
	}
	if got := Queue("in-progress", "", now); got != StatusInProgress {
		t.Fatalf("in-progress alias queue = %q, want in_progress", got)
	}
	if got := Queue("inprogress", "", now); got != StatusInProgress {
		t.Fatalf("inprogress alias queue = %q, want in_progress", got)
	}
	if got := QueueRank(StatusInProgress); got >= QueueRank(StatusNext) {
		t.Fatalf("in_progress rank %d should sort before next rank %d", got, QueueRank(StatusNext))
	}
}

func TestBindingRef(t *testing.T) {
	task := Task{ID: "task-1", ProviderRef: "remote-1", ListID: "list-a"}
	if got := BindingRef("list-b", task); got != "list-a/remote-1" {
		t.Fatalf("BindingRef() = %q", got)
	}
	task.ListID = ""
	task.ProviderRef = ""
	if got := BindingRef("list-b", task); got != "list-b/task-1" {
		t.Fatalf("BindingRef(fallback) = %q", got)
	}
}
