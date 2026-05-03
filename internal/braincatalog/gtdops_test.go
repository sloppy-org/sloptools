package braincatalog

import (
	"strings"
	"testing"
	"time"
)

func TestGTDReviewBatchQueueUsesDeterministicSignals(t *testing.T) {
	now := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	cases := []struct {
		name       string
		item       GTDListItem
		wantStatus string
		wantWhy    string
	}{
		{
			name:       "done",
			item:       GTDListItem{Status: "done"},
			wantStatus: "done",
			wantWhy:    "status=done",
		},
		{
			name:       "closed",
			item:       GTDListItem{Status: "closed"},
			wantStatus: "closed",
			wantWhy:    "status=closed",
		},
		{
			name:       "maybe stale",
			item:       GTDListItem{Status: "maybe_stale"},
			wantStatus: "review",
			wantWhy:    "status=maybe_stale",
		},
		{
			name:       "deferred future",
			item:       GTDListItem{Status: "deferred", FollowUp: "2026-05-01"},
			wantStatus: "deferred",
			wantWhy:    "follow_up future",
		},
		{
			name:       "deferred ready",
			item:       GTDListItem{Status: "deferred", FollowUp: "2026-04-01"},
			wantStatus: "next",
			wantWhy:    "follow_up reached",
		},
		{
			name:       "overdue due",
			item:       GTDListItem{Due: "2026-04-01"},
			wantStatus: "review",
			wantWhy:    "due overdue",
		},
		{
			name:       "missing status",
			item:       GTDListItem{},
			wantStatus: "inbox",
			wantWhy:    "status=inbox",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			gotStatus, gotWhy := gtdReviewBatchQueue(tc.item, now)
			if gotStatus != tc.wantStatus {
				t.Fatalf("status = %q, want %q", gotStatus, tc.wantStatus)
			}
			if gotWhy != tc.wantWhy {
				t.Fatalf("why = %q, want %q", gotWhy, tc.wantWhy)
			}
		})
	}
}

func TestSelectGTDReviewBatchItemsAppliesQueueingAndOrdering(t *testing.T) {
	now := time.Date(2026, time.April, 30, 12, 0, 0, 0, time.UTC)
	items := []GTDListItem{
		{Title: "Drop", Status: "done", Path: "drop.md"},
		{Title: "Close", Status: "closed", Path: "close.md"},
		{Title: "Inbox", Path: "inbox.md"},
		{Title: "Ada next", Status: "next", Path: "next.md"},
		{Title: "Ada review", Due: "2026-04-01", Path: "review.md"},
		{Title: "Ada deferred", Status: "deferred", FollowUp: "2026-05-01", Path: "deferred.md"},
		{Title: "Ada waiting", Status: "waiting", Path: "waiting.md"},
	}

	got := selectGTDReviewBatchItemsAt(items, "Ada", now)
	if len(got) != 4 {
		t.Fatalf("len(got) = %d, want 4: %#v", len(got), got)
	}
	want := []struct {
		title  string
		status string
	}{
		{title: "Ada next", status: "next"},
		{title: "Ada waiting", status: "waiting"},
		{title: "Ada review", status: "review"},
		{title: "Ada deferred", status: "deferred"},
	}
	for i, tc := range want {
		if got[i].Title != tc.title {
			t.Fatalf("got[%d].Title = %q, want %q", i, got[i].Title, tc.title)
		}
		if got[i].Status != tc.status {
			t.Fatalf("got[%d].Status = %q, want %q", i, got[i].Status, tc.status)
		}
	}
	joined := BuildGTDReviewBatchMarkdown(items, "work", "Ada")
	if strings.Contains(joined, "Drop") || strings.Contains(joined, "Close") {
		t.Fatalf("review batch should exclude done and closed items:\n%s", joined)
	}
}

func TestGTDQueryAndFilterMatchTrack(t *testing.T) {
	item := GTDListItem{Title: "Fix parser", Track: "software-compilers", Labels: []string{"mode/deep"}}
	if !gtdItemMatchesQuery(item, "software-compilers") {
		t.Fatal("query should match explicit track")
	}
	if !gtdListMatches(item, GTDListFilter{Track: "software-compilers"}) {
		t.Fatal("filter should match explicit track")
	}
	if gtdListMatches(item, GTDListFilter{Track: "research-fusion"}) {
		t.Fatal("filter should reject different track")
	}
}
