package email

import "context"

type ServerFilterCriteria struct {
	From          string `json:"from,omitempty"`
	To            string `json:"to,omitempty"`
	Subject       string `json:"subject,omitempty"`
	Query         string `json:"query,omitempty"`
	NegatedQuery  string `json:"negated_query,omitempty"`
	HasAttachment *bool  `json:"has_attachment,omitempty"`
}

type ServerFilterAction struct {
	Archive      bool     `json:"archive,omitempty"`
	Trash        bool     `json:"trash,omitempty"`
	MarkRead     bool     `json:"mark_read,omitempty"`
	MoveTo       string   `json:"move_to,omitempty"`
	ForwardTo    []string `json:"forward_to,omitempty"`
	AddLabels    []string `json:"add_labels,omitempty"`
	RemoveLabels []string `json:"remove_labels,omitempty"`
}

type ServerFilter struct {
	ID       string               `json:"id,omitempty"`
	Name     string               `json:"name"`
	Enabled  bool                 `json:"enabled"`
	Criteria ServerFilterCriteria `json:"criteria,omitempty"`
	Action   ServerFilterAction   `json:"action,omitempty"`
}

type ServerFilterCapabilities struct {
	Provider          string `json:"provider,omitempty"`
	SupportsList      bool   `json:"supports_list"`
	SupportsUpsert    bool   `json:"supports_upsert"`
	SupportsDelete    bool   `json:"supports_delete"`
	SupportsArchive   bool   `json:"supports_archive"`
	SupportsTrash     bool   `json:"supports_trash"`
	SupportsMoveTo    bool   `json:"supports_move_to"`
	SupportsMarkRead  bool   `json:"supports_mark_read"`
	SupportsForward   bool   `json:"supports_forward"`
	SupportsAddLabels bool   `json:"supports_add_labels"`
	SupportsQuery     bool   `json:"supports_query"`
}

type ServerFilterProvider interface {
	ServerFilterCapabilities() ServerFilterCapabilities
	ListServerFilters(context.Context) ([]ServerFilter, error)
	UpsertServerFilter(context.Context, ServerFilter) (ServerFilter, error)
	DeleteServerFilter(context.Context, string) error
}

type NamedFolderProvider interface {
	MoveToFolder(context.Context, []string, string) (int, error)
}

type NamedLabelProvider interface {
	ApplyNamedLabel(context.Context, []string, string, bool) (int, error)
}
