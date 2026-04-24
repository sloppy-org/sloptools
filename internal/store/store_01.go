package store

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/store/providerkind"
	"os"
	"os/exec"
	"strings"
	"time"
)

type ArtifactKind string

const (
	SphereWork                                  = "work"
	SpherePrivate                               = "private"
	ActorKindHuman                              = "human"
	ActorKindAgent                              = "agent"
	ArtifactKindEmail              ArtifactKind = "email"
	ArtifactKindEmailThread        ArtifactKind = "email_thread"
	ArtifactKindDocument           ArtifactKind = "document"
	ArtifactKindPDF                ArtifactKind = "pdf"
	ArtifactKindMarkdown           ArtifactKind = "markdown"
	ArtifactKindImage              ArtifactKind = "image"
	ArtifactKindGitHubIssue        ArtifactKind = "github_issue"
	ArtifactKindGitHubPR           ArtifactKind = "github_pr"
	ArtifactKindExternalTask       ArtifactKind = "external_task"
	ArtifactKindExternalNote       ArtifactKind = "external_note"
	ArtifactKindReference          ArtifactKind = "reference"
	ArtifactKindAnnotation         ArtifactKind = "annotation"
	ArtifactKindTranscript         ArtifactKind = "transcript"
	ArtifactKindPlanNote           ArtifactKind = "plan_note"
	ArtifactKindIdeaNote           ArtifactKind = "idea_note"
	ExternalProviderGmail                       = "gmail"
	ExternalProviderIMAP                        = "imap"
	ExternalProviderGoogleCalendar              = "google_calendar"
	ExternalProviderICS                         = "ics"
	ExternalProviderTodoist                     = "todoist"
	ExternalProviderEvernote                    = "evernote"
	ExternalProviderBear                        = "bear"
	ExternalProviderZotero                      = "zotero"
	ExternalProviderExchange                    = "exchange"
	ExternalProviderExchangeEWS                 = "exchange_ews"
	ItemStateInbox                              = "inbox"
	ItemStateWaiting                            = "waiting"
	ItemStateSomeday                            = "someday"
	ItemStateDone                               = "done"
	ItemReviewTargetAgent                       = "agent"
	ItemReviewTargetGitHub                      = "github"
	ItemReviewTargetEmail                       = "email"
)

type ArtifactUpdate struct {
	Kind     *ArtifactKind `json:"kind,omitempty"`
	RefPath  *string       `json:"ref_path,omitempty"`
	RefURL   *string       `json:"ref_url,omitempty"`
	Title    *string       `json:"title,omitempty"`
	MetaJSON *string       `json:"meta_json,omitempty"`
}

type ItemUpdate struct {
	Title        *string `json:"title,omitempty"`
	State        *string `json:"state,omitempty"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	Sphere       *string `json:"sphere,omitempty"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
	ReviewTarget *string `json:"review_target,omitempty"`
	Reviewer     *string `json:"reviewer,omitempty"`
}

type ItemOptions struct {
	State        string  `json:"state,omitempty"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	Sphere       *string `json:"sphere,omitempty"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
	ReviewTarget *string `json:"review_target,omitempty"`
	Reviewer     *string `json:"reviewer,omitempty"`
}

type ItemListFilter struct {
	Sphere              string `json:"sphere,omitempty"`
	Source              string `json:"source,omitempty"`
	WorkspaceID         *int64 `json:"workspace_id,omitempty"`
	WorkspaceUnassigned bool   `json:"workspace_unassigned,omitempty"`
	LabelID             *int64 `json:"label_id,omitempty"`
	Label               string `json:"label,omitempty"`
	resolvedLabelGroups [][]int64
	labelResolved       bool
}

type Label struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	Color     string `json:"color,omitempty"`
	ParentID  *int64 `json:"parent_id,omitempty"`
	CreatedAt string `json:"created_at"`
}

type Workspace struct {
	ID                       int64   `json:"id"`
	Name                     string  `json:"name"`
	DirPath                  string  `json:"dir_path"`
	Sphere                   string  `json:"sphere"`
	IsActive                 bool    `json:"is_active"`
	IsDaily                  bool    `json:"is_daily"`
	DailyDate                *string `json:"daily_date,omitempty"`
	MCPURL                   string  `json:"mcp_url,omitempty"`
	CanvasSessionID          string  `json:"canvas_session_id,omitempty"`
	ChatModel                string  `json:"chat_model,omitempty"`
	ChatModelReasoningEffort string  `json:"chat_model_reasoning_effort,omitempty"`
	CompanionConfigJSON      string  `json:"companion_config_json,omitempty"`
	Kind                     string  `json:"kind,omitempty"`
	WorkspacePath            string  `json:"workspace_path,omitempty"`
	RootPath                 string  `json:"root_path,omitempty"`
	IsDefault                bool    `json:"is_default"`
	CreatedAt                string  `json:"created_at"`
	UpdatedAt                string  `json:"updated_at"`
}

type ActorOptions struct {
	Email       *string `json:"email,omitempty"`
	Provider    *string `json:"provider,omitempty"`
	ProviderRef *string `json:"provider_ref,omitempty"`
	MetaJSON    *string `json:"meta_json,omitempty"`
}

type ExternalAccount struct {
	ID          int64  `json:"id"`
	Sphere      string `json:"sphere"`
	Provider    string `json:"provider"`
	AccountName string `json:"account_name"`
	Label       string `json:"label,omitempty"`
	ConfigJSON  string `json:"config_json"`
	Enabled     bool   `json:"enabled"`
	CreatedAt   string `json:"created_at"`
	UpdatedAt   string `json:"updated_at"`
}

type ExternalAccountUpdate struct {
	Sphere      *string        `json:"sphere,omitempty"`
	Provider    *string        `json:"provider,omitempty"`
	AccountName *string        `json:"account_name,omitempty"`
	Config      map[string]any `json:"config,omitempty"`
	Enabled     *bool          `json:"enabled,omitempty"`
}

type ExternalBinding struct {
	ID              int64   `json:"id"`
	AccountID       int64   `json:"account_id"`
	Provider        string  `json:"provider"`
	ObjectType      string  `json:"object_type"`
	RemoteID        string  `json:"remote_id"`
	ItemID          *int64  `json:"item_id,omitempty"`
	ArtifactID      *int64  `json:"artifact_id,omitempty"`
	ContainerRef    *string `json:"container_ref,omitempty"`
	RemoteUpdatedAt *string `json:"remote_updated_at,omitempty"`
	LastSyncedAt    string  `json:"last_synced_at"`
}

type ExternalContainerMapping struct {
	ID            int64   `json:"id"`
	Provider      string  `json:"provider"`
	ContainerType string  `json:"container_type"`
	ContainerRef  string  `json:"container_ref"`
	WorkspaceID   *int64  `json:"workspace_id,omitempty"`
	Sphere        *string `json:"sphere,omitempty"`
}

type ArtifactWorkspaceLink struct {
	WorkspaceID int64  `json:"workspace_id"`
	ArtifactID  int64  `json:"artifact_id"`
	CreatedAt   string `json:"created_at"`
}

type ItemArtifactLink struct {
	ItemID     int64  `json:"item_id"`
	ArtifactID int64  `json:"artifact_id"`
	Role       string `json:"role"`
	CreatedAt  string `json:"created_at"`
}

type ItemArtifact struct {
	ItemID        int64    `json:"item_id"`
	ArtifactID    int64    `json:"artifact_id"`
	Role          string   `json:"role"`
	LinkCreatedAt string   `json:"link_created_at"`
	Artifact      Artifact `json:"artifact"`
}

type Actor struct {
	ID          int64   `json:"id"`
	Name        string  `json:"name"`
	Kind        string  `json:"kind"`
	Email       *string `json:"email,omitempty"`
	Provider    *string `json:"provider,omitempty"`
	ProviderRef *string `json:"provider_ref,omitempty"`
	MetaJSON    *string `json:"meta_json,omitempty"`
	CreatedAt   string  `json:"created_at"`
}

type Artifact struct {
	ID        int64        `json:"id"`
	Kind      ArtifactKind `json:"kind"`
	RefPath   *string      `json:"ref_path,omitempty"`
	RefURL    *string      `json:"ref_url,omitempty"`
	Title     *string      `json:"title,omitempty"`
	MetaJSON  *string      `json:"meta_json,omitempty"`
	CreatedAt string       `json:"created_at"`
	UpdatedAt string       `json:"updated_at"`
}

type Item struct {
	ID           int64   `json:"id"`
	Title        string  `json:"title"`
	State        string  `json:"state"`
	WorkspaceID  *int64  `json:"workspace_id,omitempty"`
	Sphere       string  `json:"sphere"`
	ArtifactID   *int64  `json:"artifact_id,omitempty"`
	ActorID      *int64  `json:"actor_id,omitempty"`
	VisibleAfter *string `json:"visible_after,omitempty"`
	FollowUpAt   *string `json:"follow_up_at,omitempty"`
	Source       *string `json:"source,omitempty"`
	SourceRef    *string `json:"source_ref,omitempty"`
	ReviewTarget *string `json:"review_target,omitempty"`
	Reviewer     *string `json:"reviewer,omitempty"`
	ReviewedAt   *string `json:"reviewed_at,omitempty"`
	CreatedAt    string  `json:"created_at"`
	UpdatedAt    string  `json:"updated_at"`
}

type ItemSummary struct {
	Item
	ArtifactTitle *string       `json:"artifact_title,omitempty"`
	ArtifactKind  *ArtifactKind `json:"artifact_kind,omitempty"`
	ActorName     *string       `json:"actor_name,omitempty"`
}

type TimeEntry struct {
	ID          int64   `json:"id"`
	WorkspaceID *int64  `json:"workspace_id,omitempty"`
	Sphere      string  `json:"sphere"`
	StartedAt   string  `json:"started_at"`
	EndedAt     *string `json:"ended_at,omitempty"`
	Activity    string  `json:"activity,omitempty"`
	Notes       *string `json:"notes,omitempty"`
}

type TimeEntryListFilter struct {
	Sphere     string     `json:"sphere,omitempty"`
	From       *time.Time `json:"from,omitempty"`
	To         *time.Time `json:"to,omitempty"`
	ActiveOnly bool       `json:"active_only,omitempty"`
}

type TimeEntrySummary struct {
	Key         string `json:"key"`
	Label       string `json:"label"`
	Seconds     int64  `json:"seconds"`
	Duration    string `json:"duration"`
	EntryCount  int    `json:"entry_count"`
	WorkspaceID *int64 `json:"workspace_id,omitempty"`
	Sphere      string `json:"sphere,omitempty"`
}

type BatchRun struct {
	ID          int64   `json:"id"`
	WorkspaceID int64   `json:"workspace_id"`
	StartedAt   string  `json:"started_at"`
	FinishedAt  *string `json:"finished_at,omitempty"`
	ConfigJSON  string  `json:"config_json"`
	Status      string  `json:"status"`
}

type BatchRunItem struct {
	BatchID    int64   `json:"batch_id"`
	ItemID     int64   `json:"item_id"`
	ItemTitle  *string `json:"item_title,omitempty"`
	Status     string  `json:"status"`
	PRNumber   *int64  `json:"pr_number,omitempty"`
	PRURL      *string `json:"pr_url,omitempty"`
	ErrorMsg   *string `json:"error_msg,omitempty"`
	StartedAt  *string `json:"started_at,omitempty"`
	FinishedAt *string `json:"finished_at,omitempty"`
}

type BatchRunItemUpdate struct {
	Status     string  `json:"status"`
	PRNumber   *int64  `json:"pr_number,omitempty"`
	PRURL      *string `json:"pr_url,omitempty"`
	ErrorMsg   *string `json:"error_msg,omitempty"`
	StartedAt  *string `json:"started_at,omitempty"`
	FinishedAt *string `json:"finished_at,omitempty"`
}

type WorkspaceWatch struct {
	WorkspaceID         int64  `json:"workspace_id"`
	ConfigJSON          string `json:"config_json"`
	PollIntervalSeconds int    `json:"poll_interval_seconds"`
	Enabled             bool   `json:"enabled"`
	CurrentBatchID      *int64 `json:"current_batch_id,omitempty"`
	CreatedAt           string `json:"created_at"`
	UpdatedAt           string `json:"updated_at"`
}

const (
	externalAccountCredentialSourceEnv       = "env"
	externalAccountCredentialSourceBitwarden = "bitwarden"
)

var errExternalAccountPasswordUnavailable = errors.New("external account password is not configured")

type externalAccountCommandRunner func(context.Context, string, ...string) (string, error)

type cachedExternalAccountCredential struct {
	value  string
	source string
}

func runExternalAccountCommand(ctx context.Context, name string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, name, args...)
	output, err := cmd.Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			if stderr := strings.TrimSpace(string(exitErr.Stderr)); stderr != "" {
				return "", errors.New(stderr)
			}
		}
		return "", err
	}
	return string(output), nil
}

func decodeExternalAccountConfigJSON(raw string) (map[string]any, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return map[string]any{}, nil
	}
	var config map[string]any
	if err := json.Unmarshal([]byte(clean), &config); err != nil {
		return nil, fmt.Errorf("decode external account config: %w", err)
	}
	if config == nil {
		config = map[string]any{}
	}
	return config, nil
}

func externalAccountCredentialRef(config map[string]any) string {
	raw, _ := config["credential_ref"].(string)
	return strings.TrimSpace(raw)
}

func bitwardenItemNameFromCredentialRef(ref string) (string, error) {
	clean := strings.TrimSpace(ref)
	if clean == "" {
		return "", errors.New("credential_ref is required")
	}
	if !strings.HasPrefix(strings.ToLower(clean), "bw://") {
		return "", fmt.Errorf("unsupported credential_ref %q", clean)
	}
	itemName := strings.TrimLeft(clean[len("bw://"):], "/")
	itemName = strings.TrimSpace(itemName)
	if itemName == "" {
		return "", errors.New("bitwarden credential_ref must include an item name")
	}
	return itemName, nil
}

func trimSecretOutput(raw string) string {
	return strings.TrimRight(raw, "\r\n")
}

func externalAccountCredentialCacheKey(account ExternalAccount, credentialRef string) string {
	return strings.Join([]string{strings.TrimSpace(account.Provider), strings.TrimSpace(account.AccountName), strings.TrimSpace(credentialRef)}, "\x00")
}

func (s *Store) cachedExternalAccountPassword(key string) (cachedExternalAccountCredential, bool) {
	s.externalAccountCredentialMu.Lock()
	defer s.externalAccountCredentialMu.Unlock()
	entry, ok := s.externalAccountCredentialCache[key]
	return entry, ok
}

func (s *Store) cacheExternalAccountPassword(key, source, value string) {
	if key == "" || value == "" {
		return
	}
	s.externalAccountCredentialMu.Lock()
	defer s.externalAccountCredentialMu.Unlock()
	if s.externalAccountCredentialCache == nil {
		s.externalAccountCredentialCache = map[string]cachedExternalAccountCredential{}
	}
	s.externalAccountCredentialCache[key] = cachedExternalAccountCredential{value: value, source: source}
}

func (s *Store) lookupExternalAccountEnv(key string) (string, bool) {
	if s.externalAccountLookupEnv != nil {
		return s.externalAccountLookupEnv(key)
	}
	return os.LookupEnv(key)
}

func (s *Store) runExternalAccountCredentialCommand(ctx context.Context, name string, args ...string) (string, error) {
	if s.externalAccountCommandRunner != nil {
		return s.externalAccountCommandRunner(ctx, name, args...)
	}
	return runExternalAccountCommand(ctx, name, args...)
}

func (s *Store) resolveBitwardenPassword(ctx context.Context, credentialRef string) (string, error) {
	itemName, err := bitwardenItemNameFromCredentialRef(credentialRef)
	if err != nil {
		return "", err
	}
	output, err := s.runExternalAccountCredentialCommand(ctx, "bw", "get", "password", itemName)
	if err != nil {
		return "", fmt.Errorf("resolve bitwarden credential %q: %w", itemName, err)
	}
	password := trimSecretOutput(output)
	if password == "" {
		return "", fmt.Errorf("resolve bitwarden credential %q: empty password", itemName)
	}
	return password, nil
}

func (s *Store) ResolveExternalAccountPassword(ctx context.Context, accountID int64) (string, string, error) {
	account, err := s.GetExternalAccount(accountID)
	if err != nil {
		return "", "", err
	}
	return s.ResolveExternalAccountPasswordForAccount(ctx, account)
}

func (s *Store) ResolveExternalAccountPasswordForAccount(ctx context.Context, account ExternalAccount) (string, string, error) {
	envVar := ExternalAccountPasswordEnvVar(account.Provider, account.AccountName)
	config, err := decodeExternalAccountConfigJSON(account.ConfigJSON)
	if err != nil {
		return "", "", err
	}
	credentialRef := externalAccountCredentialRef(config)
	cacheKey := externalAccountCredentialCacheKey(account, credentialRef)
	if value, ok := s.lookupExternalAccountEnv(envVar); ok && value != "" {
		s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceEnv, value)
		return value, externalAccountCredentialSourceEnv, nil
	}
	if cached, ok := s.cachedExternalAccountPassword(cacheKey); ok {
		return cached.value, cached.source, nil
	}
	if credentialRef == "" {
		return "", "", errExternalAccountPasswordUnavailable
	}
	password, err := s.resolveBitwardenPassword(ctx, credentialRef)
	if err != nil {
		return "", "", err
	}
	s.cacheExternalAccountPassword(cacheKey, externalAccountCredentialSourceBitwarden, password)
	return password, externalAccountCredentialSourceBitwarden, nil
}

func IsEmailProvider(provider string) bool {
	return providerkind.IsEmail(provider)
}

func IsManagedEmailProvider(provider string) bool {
	return providerkind.IsManagedEmail(provider)
}
