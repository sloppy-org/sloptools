package store

import (
	"errors"
	"strings"
)

type MailTriageReview struct {
	ID        int64  `json:"id"`
	AccountID int64  `json:"account_id"`
	Provider  string `json:"provider"`
	MessageID string `json:"message_id"`
	Folder    string `json:"folder"`
	Subject   string `json:"subject"`
	Sender    string `json:"sender"`
	Action    string `json:"action"`
	CreatedAt string `json:"created_at"`
}

type MailTriageReviewInput struct {
	AccountID int64
	Provider  string
	MessageID string
	Folder    string
	Subject   string
	Sender    string
	Action    string
}

func normalizeMailTriageReviewAction(raw string) string {
	switch strings.ToLower(strings.TrimSpace(raw)) {
	case "inbox":
		return "inbox"
	case "cc":
		return "cc"
	case "archive":
		return "archive"
	case "trash":
		return "trash"
	default:
		return ""
	}
}

func (s *Store) CreateMailTriageReview(input MailTriageReviewInput) (MailTriageReview, error) {
	if input.AccountID <= 0 {
		return MailTriageReview{}, errors.New("account_id is required")
	}
	provider := normalizeExternalAccountProvider(input.Provider)
	if provider == "" {
		return MailTriageReview{}, errors.New("provider is required")
	}
	messageID := strings.TrimSpace(input.MessageID)
	if messageID == "" {
		return MailTriageReview{}, errors.New("message_id is required")
	}
	action := normalizeMailTriageReviewAction(input.Action)
	if action == "" {
		return MailTriageReview{}, errors.New("action must be inbox, cc, archive, or trash")
	}
	result, err := s.db.Exec(`INSERT INTO mail_triage_reviews (
	  account_id, provider, message_id, folder, subject, sender, action
	) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		input.AccountID,
		provider,
		messageID,
		strings.TrimSpace(input.Folder),
		strings.TrimSpace(input.Subject),
		strings.TrimSpace(input.Sender),
		action,
	)
	if err != nil {
		return MailTriageReview{}, err
	}
	id, err := result.LastInsertId()
	if err != nil {
		return MailTriageReview{}, err
	}
	return s.GetMailTriageReview(id)
}

func (s *Store) GetMailTriageReview(id int64) (MailTriageReview, error) {
	if id <= 0 {
		return MailTriageReview{}, errors.New("id is required")
	}
	var review MailTriageReview
	err := s.db.QueryRow(`SELECT id, account_id, provider, message_id, folder, subject, sender, action, created_at
FROM mail_triage_reviews
WHERE id = ?`, id).Scan(
		&review.ID,
		&review.AccountID,
		&review.Provider,
		&review.MessageID,
		&review.Folder,
		&review.Subject,
		&review.Sender,
		&review.Action,
		&review.CreatedAt,
	)
	if err != nil {
		return MailTriageReview{}, err
	}
	return review, nil
}

func (s *Store) ListMailTriageReviews(accountID int64, limit int) ([]MailTriageReview, error) {
	if accountID <= 0 {
		return nil, errors.New("account_id is required")
	}
	if limit <= 0 {
		limit = 100
	}
	rows, err := s.db.Query(`SELECT id, account_id, provider, message_id, folder, subject, sender, action, created_at
FROM mail_triage_reviews
WHERE account_id = ?
ORDER BY datetime(created_at) DESC, id DESC
LIMIT ?`, accountID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reviews []MailTriageReview
	for rows.Next() {
		var review MailTriageReview
		if err := rows.Scan(
			&review.ID,
			&review.AccountID,
			&review.Provider,
			&review.MessageID,
			&review.Folder,
			&review.Subject,
			&review.Sender,
			&review.Action,
			&review.CreatedAt,
		); err != nil {
			return nil, err
		}
		reviews = append(reviews, review)
	}
	return reviews, rows.Err()
}

func (s *Store) ListMailTriageReviewedMessageIDs(accountID int64, folder string, limit int) ([]string, error) {
	if accountID <= 0 {
		return nil, errors.New("account_id is required")
	}
	if limit <= 0 {
		limit = 5000
	}
	cleanFolder := strings.TrimSpace(folder)
	rows, err := s.db.Query(`SELECT message_id
FROM mail_triage_reviews
WHERE account_id = ?
  AND (? = '' OR lower(folder) = lower(?))
GROUP BY message_id
ORDER BY max(datetime(created_at)) DESC, message_id DESC
LIMIT ?`, accountID, cleanFolder, cleanFolder, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var messageID string
		if err := rows.Scan(&messageID); err != nil {
			return nil, err
		}
		if clean := strings.TrimSpace(messageID); clean != "" {
			ids = append(ids, clean)
		}
	}
	return ids, rows.Err()
}

func (s *Store) DeleteMailTriageReview(id int64) error {
	if id <= 0 {
		return errors.New("id is required")
	}
	_, err := s.db.Exec(`DELETE FROM mail_triage_reviews WHERE id = ?`, id)
	return err
}
