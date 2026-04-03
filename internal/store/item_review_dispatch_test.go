package store

import "testing"

func TestUpdateItemReviewDispatch(t *testing.T) {
	s := newTestStore(t)

	item, err := s.CreateItem("Review parser PR", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}

	target := ItemReviewTargetGitHub
	reviewer := "alice"
	if err := s.UpdateItemReviewDispatch(item.ID, &target, &reviewer); err != nil {
		t.Fatalf("UpdateItemReviewDispatch() error: %v", err)
	}

	got, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem() error: %v", err)
	}
	if got.ReviewTarget == nil || *got.ReviewTarget != target {
		t.Fatalf("review_target = %v, want %q", got.ReviewTarget, target)
	}
	if got.Reviewer == nil || *got.Reviewer != reviewer {
		t.Fatalf("reviewer = %v, want %q", got.Reviewer, reviewer)
	}
	if got.ReviewedAt == nil || *got.ReviewedAt == "" {
		t.Fatalf("reviewed_at = %v, want timestamp", got.ReviewedAt)
	}

	if err := s.UpdateItemReviewDispatch(item.ID, nil, nil); err != nil {
		t.Fatalf("UpdateItemReviewDispatch(clear) error: %v", err)
	}
	cleared, err := s.GetItem(item.ID)
	if err != nil {
		t.Fatalf("GetItem(clear) error: %v", err)
	}
	if cleared.ReviewTarget != nil || cleared.Reviewer != nil || cleared.ReviewedAt != nil {
		t.Fatalf("cleared dispatch = %+v", cleared)
	}
}

func TestUpdateItemReviewDispatchRejectsReviewerWithoutTarget(t *testing.T) {
	s := newTestStore(t)

	item, err := s.CreateItem("Review parser PR", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	reviewer := "alice"
	if err := s.UpdateItemReviewDispatch(item.ID, nil, &reviewer); err == nil {
		t.Fatal("expected reviewer-without-target error")
	}
}
