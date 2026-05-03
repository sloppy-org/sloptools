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
