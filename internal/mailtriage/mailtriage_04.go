package mailtriage

import (
	"time"
)

type Message struct {
	ID             string    `json:"id"`
	Provider       string    `json:"provider,omitempty"`
	AccountLabel   string    `json:"account_label,omitempty"`
	AccountAddress string    `json:"account_address,omitempty"`
	ThreadID       string    `json:"thread_id,omitempty"`
	Subject        string    `json:"subject,omitempty"`
	Sender         string    `json:"sender,omitempty"`
	Recipients     []string  `json:"recipients,omitempty"`
	Labels         []string  `json:"labels,omitempty"`
	Snippet        string    `json:"snippet,omitempty"`
	Body           string    `json:"body,omitempty"`
	HasAttachments bool      `json:"has_attachments,omitempty"`
	IsRead         bool      `json:"is_read,omitempty"`
	IsFlagged      bool      `json:"is_flagged,omitempty"`
	ReceivedAt     time.Time `json:"received_at,omitempty"`
	ReviewCount    int       `json:"review_count,omitempty"`
	PolicySummary  []string  `json:"policy_summary,omitempty"`
	Examples       []Example `json:"examples,omitempty"`
	LocalHints     []string  `json:"local_hints,omitempty"`
	ProtectedTopic bool      `json:"protected_topic,omitempty"`
	AgeDays        int       `json:"age_days,omitempty"`
}

type Example struct {
	Sender  string `json:"sender,omitempty"`
	Subject string `json:"subject,omitempty"`
	Folder  string `json:"folder,omitempty"`
	Action  string `json:"action,omitempty"`
}

type ReviewedExample struct {
	Sender  string `json:"sender,omitempty"`
	Subject string `json:"subject,omitempty"`
	Folder  string `json:"folder,omitempty"`
	Action  string `json:"action,omitempty"`
}

type DistilledTraining struct {
	ReviewCount        int                 `json:"review_count,omitempty"`
	PolicySummary      []string            `json:"policy_summary,omitempty"`
	Examples           []Example           `json:"examples,omitempty"`
	DeterministicRules []DeterministicRule `json:"deterministic_rules,omitempty"`
	Warnings           []string            `json:"warnings,omitempty"`
	Report             TrainingReport      `json:"report,omitempty"`
	Model              *TrainingModel      `json:"-"`
}

type FactorScores struct {
	Spam           float64 `json:"spam,omitempty"`
	ActionRequired float64 `json:"action_required,omitempty"`
	Skim           float64 `json:"skim,omitempty"`
	Reference      float64 `json:"reference,omitempty"`
	Staleness      float64 `json:"staleness,omitempty"`
}

type DeterministicRule struct {
	Scope   string  `json:"scope,omitempty"`
	Key     string  `json:"key,omitempty"`
	Action  Action  `json:"action,omitempty"`
	Support int     `json:"support,omitempty"`
	Purity  float64 `json:"purity,omitempty"`
	Reason  string  `json:"reason,omitempty"`
}

type InconsistentPattern struct {
	Scope   string   `json:"scope,omitempty"`
	Key     string   `json:"key,omitempty"`
	Count   int      `json:"count,omitempty"`
	Actions []string `json:"actions,omitempty"`
}

type TrainingReport struct {
	ReviewCount          int                   `json:"review_count,omitempty"`
	ActionCounts         map[string]int        `json:"action_counts,omitempty"`
	DeterministicRules   []DeterministicRule   `json:"deterministic_rules,omitempty"`
	InconsistentPatterns []InconsistentPattern `json:"inconsistent_patterns,omitempty"`
	ProtectedTopics      []string              `json:"protected_topics,omitempty"`
}

type Decision struct {
	Action       Action       `json:"action"`
	ArchiveLabel string       `json:"archive_label,omitempty"`
	Confidence   float64      `json:"confidence"`
	Reason       string       `json:"reason,omitempty"`
	Signals      []string     `json:"signals,omitempty"`
	Model        string       `json:"model,omitempty"`
	Factors      FactorScores `json:"factors,omitempty"`
}

type Policy struct {
	Phase                     Phase              `json:"phase"`
	ReviewOnAuditDisagreement bool               `json:"review_on_audit_disagreement"`
	AutoApplyMinConfidence    map[Action]float64 `json:"auto_apply_min_confidence,omitempty"`
	ManualActions             []Action           `json:"manual_actions,omitempty"`
}

type Evaluation struct {
	Message        Message     `json:"message"`
	Primary        Decision    `json:"primary"`
	Audit          *Decision   `json:"audit,omitempty"`
	Disposition    Disposition `json:"disposition"`
	ReviewRequired bool        `json:"review_required"`
	ReviewReasons  []string    `json:"review_reasons,omitempty"`
}

func DefaultPolicy(phase Phase) Policy {
	if phase == "" {
		phase = PhaseManualReview
	}
	return Policy{Phase: phase, ReviewOnAuditDisagreement: true, AutoApplyMinConfidence: map[Action]float64{ActionCC: 0.90, ActionArchive: 0.93, ActionTrash: 0.98}}
}
