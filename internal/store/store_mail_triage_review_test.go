package store

import (
	"path/filepath"
	"testing"
)

func TestCreateAndListMailTriageReviews(t *testing.T) {
	s, err := New(filepath.Join(t.TempDir(), "triage.db"))
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	defer s.Close()

	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "TU Graz", nil)
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	first, err := s.CreateMailTriageReview(MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m1",
		Folder:    "Posteingang",
		Subject:   "One",
		Sender:    "alice@example.com",
		Action:    "inbox",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(first) error: %v", err)
	}
	second, err := s.CreateMailTriageReview(MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m2",
		Folder:    "Junk-E-Mail",
		Subject:   "Two",
		Sender:    "spam@example.com",
		Action:    "trash",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(second) error: %v", err)
	}
	third, err := s.CreateMailTriageReview(MailTriageReviewInput{
		AccountID: account.ID,
		Provider:  account.Provider,
		MessageID: "m3",
		Folder:    "Posteingang",
		Subject:   "Three",
		Sender:    "list@example.com",
		Action:    "cc",
	})
	if err != nil {
		t.Fatalf("CreateMailTriageReview(third) error: %v", err)
	}
	if first.ID <= 0 || second.ID <= 0 || third.ID <= 0 {
		t.Fatalf("review ids = %d, %d, %d", first.ID, second.ID, third.ID)
	}

	reviews, err := s.ListMailTriageReviews(account.ID, 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviews() error: %v", err)
	}
	if len(reviews) != 3 {
		t.Fatalf("reviews len = %d, want 3", len(reviews))
	}
	if reviews[0].MessageID != "m3" || reviews[0].Action != "cc" {
		t.Fatalf("reviews[0] = %#v", reviews[0])
	}
	if reviews[1].MessageID != "m2" || reviews[1].Action != "trash" {
		t.Fatalf("reviews[1] = %#v", reviews[1])
	}
	if reviews[2].MessageID != "m1" || reviews[2].Action != "inbox" {
		t.Fatalf("reviews[2] = %#v", reviews[2])
	}

	ids, err := s.ListMailTriageReviewedMessageIDs(account.ID, "Junk-E-Mail", 10)
	if err != nil {
		t.Fatalf("ListMailTriageReviewedMessageIDs() error: %v", err)
	}
	if len(ids) != 1 || ids[0] != "m2" {
		t.Fatalf("reviewed ids = %#v, want [m2]", ids)
	}
}
