package store

import "time"

type ArtifactKind string

const (
	SphereWork    = "work"
	SpherePrivate = "private"

	ActorKindHuman = "human"
	ActorKindAgent = "agent"

	ArtifactKindEmail        ArtifactKind = "email"
	ArtifactKindEmailThread  ArtifactKind = "email_thread"
	ArtifactKindDocument     ArtifactKind = "document"
	ArtifactKindPDF          ArtifactKind = "pdf"
	ArtifactKindMarkdown     ArtifactKind = "markdown"
	ArtifactKindImage        ArtifactKind = "image"
	ArtifactKindGitHubIssue  ArtifactKind = "github_issue"
	ArtifactKindGitHubPR     ArtifactKind = "github_pr"
	ArtifactKindExternalTask ArtifactKind = "external_task"
	ArtifactKindExternalNote ArtifactKind = "external_note"
	ArtifactKindReference    ArtifactKind = "reference"
	ArtifactKindAnnotation   ArtifactKind = "annotation"
	ArtifactKindTranscript   ArtifactKind = "transcript"
	ArtifactKindPlanNote     ArtifactKind = "plan_note"
	ArtifactKindIdeaNote     ArtifactKind = "idea_note"

	ExternalProviderGmail          = "gmail"
	ExternalProviderIMAP           = "imap"
	ExternalProviderGoogleCalendar = "google_calendar"
	ExternalProviderICS            = "ics"
	ExternalProviderTodoist        = "todoist"
	ExternalProviderEvernote       = "evernote"
	ExternalProviderBear           = "bear"
	ExternalProviderZotero         = "zotero"
	ExternalProviderExchange       = "exchange"
	ExternalProviderExchangeEWS    = "exchange_ews"

	ItemStateInbox   = "inbox"
	ItemStateWaiting = "waiting"
	ItemStateSomeday = "someday"
	ItemStateDone    = "done"

	ItemReviewTargetAgent  = "agent"
	ItemReviewTargetGitHub = "github"
	ItemReviewTargetEmail  = "email"
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
	Kind          string `json:"kind,omitempty"`
	WorkspacePath string `json:"workspace_path,omitempty"`
	RootPath      string `json:"root_path,omitempty"`
	IsDefault     bool   `json:"is_default"`
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
