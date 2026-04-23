package surface

import "strings"

type ToolProperty struct {
	Type        string
	Description string
	Enum        []string
}

type Tool struct {
	Name        string
	Description string
	Required    []string
	Properties  map[string]ToolProperty
}

type RouteSection struct {
	Title  string
	Routes []string
}

var MCPTools = []Tool{
	{
		Name:        "canvas_session_open",
		Description: "Open canvas session and initialize runtime status.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_artifact_show",
		Description: "Show one artifact kind in canvas: text, image, pdf, or clear.",
		Required:    []string{"session_id", "kind"},
	},
	{
		Name:        "canvas_status",
		Description: "Get current session status and active artifact metadata.",
		Required:    []string{"session_id"},
	},
	{
		Name:        "canvas_import_handoff",
		Description: "Consume a generic producer handoff and render it in canvas.",
		Required:    []string{"session_id", "handoff_id"},
	},
	{
		Name:        "handoff.create",
		Description: "Create a producer handoff. Sloppy currently supports kind=mail with selector.account_id plus message_id or message_ids.",
		Required:    []string{"kind", "selector"},
		Properties: map[string]ToolProperty{
			"kind": {
				Type:        "string",
				Description: "Handoff kind. Sloppy currently supports mail.",
				Enum:        []string{"mail"},
			},
			"selector": {
				Type:        "object",
				Description: "Kind-specific source selection. For mail, provide account_id and message_id or message_ids.",
			},
			"policy": {
				Type:        "object",
				Description: "Optional lifecycle policy with ttl_seconds, expires_at, and max_consumes.",
			},
		},
	},
	{
		Name:        "handoff.peek",
		Description: "Read producer handoff metadata without payload bytes.",
		Required:    []string{"handoff_id"},
		Properties: map[string]ToolProperty{
			"handoff_id": {
				Type:        "string",
				Description: "Producer handoff identifier.",
			},
		},
	},
	{
		Name:        "handoff.consume",
		Description: "Consume one producer handoff payload and advance policy counters.",
		Required:    []string{"handoff_id"},
		Properties: map[string]ToolProperty{
			"handoff_id": {
				Type:        "string",
				Description: "Producer handoff identifier.",
			},
		},
	},
	{
		Name:        "handoff.revoke",
		Description: "Revoke one producer handoff so future consumes are rejected.",
		Required:    []string{"handoff_id"},
		Properties: map[string]ToolProperty{
			"handoff_id": {
				Type:        "string",
				Description: "Producer handoff identifier.",
			},
		},
	},
	{
		Name:        "handoff.status",
		Description: "Show producer handoff metadata plus lifecycle counters and revocation state.",
		Required:    []string{"handoff_id"},
		Properties: map[string]ToolProperty{
			"handoff_id": {
				Type:        "string",
				Description: "Producer handoff identifier.",
			},
		},
	},
	{
		Name:        "temp_file_create",
		Description: "Create a temporary file under .sloptools/artifacts/tmp for file-backed artifact usage.",
		Properties: map[string]ToolProperty{
			"cwd": {
				Type:        "string",
				Description: "Project root to create the temp file under. Defaults to active project root.",
			},
			"prefix": {
				Type:        "string",
				Description: "Filename prefix for the generated temp file.",
			},
			"suffix": {
				Type:        "string",
				Description: "Filename suffix/extension (for example .md). Default is .md.",
			},
			"content": {
				Type:        "string",
				Description: "Optional initial file content.",
			},
		},
	},
	{
		Name:        "temp_file_remove",
		Description: "Remove a temporary file previously created under .sloptools/artifacts/tmp.",
		Required:    []string{"path"},
		Properties: map[string]ToolProperty{
			"path": {
				Type:        "string",
				Description: "Relative or absolute temp file path to remove.",
			},
			"cwd": {
				Type:        "string",
				Description: "Project root for resolving relative paths. Defaults to active project root.",
			},
		},
	},
	{
		Name:        "workspace_list",
		Description: "List workspaces, optionally filtered by the legacy work/private top-level context parameter.",
		Properties: map[string]ToolProperty{
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
		},
	},
	{
		Name:        "workspace_activate",
		Description: "Set the active workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to activate.",
			},
		},
	},
	{
		Name:        "workspace_get",
		Description: "Get workspace details and open item counts.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to inspect.",
			},
		},
	},
	{
		Name:        "workspace_watch_start",
		Description: "Enable watch mode for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to watch.",
			},
			"poll_interval_seconds": {
				Type:        "integer",
				Description: "Optional polling interval in seconds.",
			},
			"config_json": {
				Type:        "string",
				Description: "Optional JSON config for worker selection.",
			},
		},
	},
	{
		Name:        "workspace_watch_stop",
		Description: "Disable watch mode for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to stop watching.",
			},
		},
	},
	{
		Name:        "workspace_watch_status",
		Description: "Get persisted watch status for a workspace.",
		Required:    []string{"workspace_id"},
		Properties: map[string]ToolProperty{
			"workspace_id": {
				Type:        "integer",
				Description: "Workspace id to inspect.",
			},
		},
	},
	{
		Name:        "item_list",
		Description: "List items, optionally filtered by state, workspace, legacy work/private top-level context, or source.",
		Properties: map[string]ToolProperty{
			"state": {
				Type:        "string",
				Description: "Optional item state filter.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id filter. Use 0 for unassigned items.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
			"source": {
				Type:        "string",
				Description: "Optional source filter.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of items to return.",
			},
		},
	},
	{
		Name:        "item_get",
		Description: "Get an item with linked workspace, actor, and artifact details.",
		Required:    []string{"item_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to inspect.",
			},
		},
	},
	{
		Name:        "item_create",
		Description: "Create a new item with optional workspace, artifact, actor, and timing links.",
		Required:    []string{"title"},
		Properties: map[string]ToolProperty{
			"title": {
				Type:        "string",
				Description: "Item title.",
			},
			"state": {
				Type:        "string",
				Description: "Optional initial state. Defaults to inbox.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id.",
			},
			"artifact_id": {
				Type:        "integer",
				Description: "Optional primary artifact id.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Optional actor id.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context override.",
				Enum:        []string{"work", "private"},
			},
			"visible_after": {
				Type:        "string",
				Description: "Optional RFC3339 visibility timestamp.",
			},
			"follow_up_at": {
				Type:        "string",
				Description: "Optional RFC3339 follow-up timestamp.",
			},
			"source": {
				Type:        "string",
				Description: "Optional source provider name.",
			},
			"source_ref": {
				Type:        "string",
				Description: "Optional provider-specific source reference.",
			},
		},
	},
	{
		Name:        "item_triage",
		Description: "Triage an item to done, later, delegate, someday, or delete.",
		Required:    []string{"item_id", "action"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to triage.",
			},
			"action": {
				Type:        "string",
				Description: "Triage action.",
				Enum:        []string{"done", "later", "delegate", "someday", "delete"},
			},
			"actor_id": {
				Type:        "integer",
				Description: "Required when action=delegate.",
			},
			"visible_after": {
				Type:        "string",
				Description: "Required when action=later, in RFC3339 format.",
			},
		},
	},
	{
		Name:        "item_assign",
		Description: "Assign an item to an actor.",
		Required:    []string{"item_id", "actor_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to assign.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Actor id to assign.",
			},
		},
	},
	{
		Name:        "item_update",
		Description: "Update an item's title, state, links, source, or timing fields.",
		Required:    []string{"item_id"},
		Properties: map[string]ToolProperty{
			"item_id": {
				Type:        "integer",
				Description: "Item id to update.",
			},
			"title": {
				Type:        "string",
				Description: "Optional updated title.",
			},
			"state": {
				Type:        "string",
				Description: "Optional updated state.",
				Enum:        []string{"inbox", "waiting", "someday", "done"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id. Use 0 to clear.",
			},
			"artifact_id": {
				Type:        "integer",
				Description: "Optional primary artifact id. Use 0 to clear.",
			},
			"actor_id": {
				Type:        "integer",
				Description: "Optional actor id. Use 0 to clear.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context override.",
				Enum:        []string{"work", "private"},
			},
			"visible_after": {
				Type:        "string",
				Description: "Optional RFC3339 visibility timestamp.",
			},
			"follow_up_at": {
				Type:        "string",
				Description: "Optional RFC3339 follow-up timestamp.",
			},
			"source": {
				Type:        "string",
				Description: "Optional provider source name.",
			},
			"source_ref": {
				Type:        "string",
				Description: "Optional provider source reference.",
			},
		},
	},
	{
		Name:        "artifact_get",
		Description: "Get an artifact with linked items and readable local text content when available.",
		Required:    []string{"artifact_id"},
		Properties: map[string]ToolProperty{
			"artifact_id": {
				Type:        "integer",
				Description: "Artifact id to inspect.",
			},
		},
	},
	{
		Name:        "artifact_list",
		Description: "List artifacts, optionally filtered by kind or workspace.",
		Properties: map[string]ToolProperty{
			"kind": {
				Type:        "string",
				Description: "Optional artifact kind filter.",
				Enum:        []string{"email", "email_thread", "document", "pdf", "markdown", "image", "github_issue", "github_pr", "external_task", "transcript", "plan_note", "idea_note"},
			},
			"workspace_id": {
				Type:        "integer",
				Description: "Optional workspace id filter.",
			},
			"linked_only": {
				Type:        "boolean",
				Description: "Only include explicitly linked workspace artifacts.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of artifacts to return.",
			},
		},
	},
	{
		Name:        "actor_list",
		Description: "List actors.",
	},
	{
		Name:        "actor_create",
		Description: "Create an actor.",
		Required:    []string{"name", "kind"},
		Properties: map[string]ToolProperty{
			"name": {
				Type:        "string",
				Description: "Actor display name.",
			},
			"kind": {
				Type:        "string",
				Description: "Actor kind.",
				Enum:        []string{"human", "agent"},
			},
		},
	},
	{
		Name:        "calendar_list",
		Description: "List Google calendars available through Sloppy's shared Google auth.",
	},
	{
		Name:        "calendar_events",
		Description: "List upcoming Google Calendar events with optional calendar, query, day-window, and limit filters.",
		Properties: map[string]ToolProperty{
			"calendar_id": {
				Type:        "string",
				Description: "Optional Google Calendar id. When omitted, events from all calendars are returned.",
			},
			"days": {
				Type:        "integer",
				Description: "Optional number of days forward from now to include. Defaults to 30.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of events to return. Defaults to 100.",
			},
			"query": {
				Type:        "string",
				Description: "Optional free-text search query applied by Google Calendar.",
			},
		},
	},
	{
		Name:        "calendar_event_create",
		Description: "Create a Google Calendar event.",
		Required:    []string{"summary", "start"},
		Properties: map[string]ToolProperty{
			"calendar_id": {
				Type:        "string",
				Description: "Optional Google Calendar id or name. When omitted, Sloppy chooses the best matching calendar.",
			},
			"sphere": {
				Type:        "string",
				Description: "Optional preferred sphere used when selecting a default calendar.",
				Enum:        []string{"work", "private"},
			},
			"summary": {
				Type:        "string",
				Description: "Event title.",
			},
			"start": {
				Type:        "string",
				Description: "Start time as RFC3339, local YYYY-MM-DDTHH:MM, or YYYY-MM-DD for all-day events.",
			},
			"end": {
				Type:        "string",
				Description: "Optional end time in the same format as start.",
			},
			"duration_minutes": {
				Type:        "integer",
				Description: "Optional duration in minutes when end is omitted. Defaults to 60 for timed events.",
			},
			"description": {
				Type:        "string",
				Description: "Optional event description.",
			},
			"location": {
				Type:        "string",
				Description: "Optional event location.",
			},
			"attendees": {
				Type:        "array",
				Description: "Optional attendee email addresses.",
			},
			"all_day": {
				Type:        "boolean",
				Description: "Create an all-day event. If true and end is omitted, the event spans one day.",
			},
		},
	},
	{
		Name:        "mail_account_list",
		Description: "List enabled email accounts available through Sloppy.",
		Properties: map[string]ToolProperty{
			"sphere": {
				Type:        "string",
				Description: "Optional legacy work/private top-level context filter.",
				Enum:        []string{"work", "private"},
			},
		},
	},
	{
		Name:        "mail_label_list",
		Description: "List labels or folders for a mail account.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
		},
	},
	{
		Name:        "mail_message_list",
		Description: "List messages from a mail account, newest first, with mailbox filters and paging. Returns up to 50 messages per page (default 20). Use next_page_token to fetch more.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"folder": {
				Type:        "string",
				Description: "Optional folder or label scope.",
			},
			"text": {
				Type:        "string",
				Description: "Optional free-text search.",
			},
			"subject": {
				Type:        "string",
				Description: "Optional subject filter.",
			},
			"from": {
				Type:        "string",
				Description: "Optional sender filter.",
			},
			"to": {
				Type:        "string",
				Description: "Optional recipient filter.",
			},
			"days": {
				Type:        "integer",
				Description: "Optional rolling day window from now.",
			},
			"after": {
				Type:        "string",
				Description: "Optional RFC3339 lower bound.",
			},
			"before": {
				Type:        "string",
				Description: "Optional RFC3339 upper bound.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of messages to return.",
			},
			"page_token": {
				Type:        "string",
				Description: "Optional provider page token for the next slice.",
			},
			"include_spam_trash": {
				Type:        "boolean",
				Description: "Include junk/spam/trash when supported.",
			},
			"has_attachment": {
				Type:        "boolean",
				Description: "Filter by attachment presence.",
			},
			"is_read": {
				Type:        "boolean",
				Description: "Filter by read state.",
			},
			"is_flagged": {
				Type:        "boolean",
				Description: "Filter by flagged state.",
			},
		},
	},
	{
		Name:        "mail_message_get",
		Description: "Get one full message from a mail account.",
		Required:    []string{"account_id", "message_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"message_id": {
				Type:        "string",
				Description: "Provider message id.",
			},
		},
	},
	{
		Name:        "mail_attachment_get",
		Description: "Download one mail attachment to disk and return the absolute path. Never returns the file bytes inline so large attachments stay out of agent context.",
		Required:    []string{"account_id", "message_id", "attachment_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"message_id": {
				Type:        "string",
				Description: "Provider message id.",
			},
			"attachment_id": {
				Type:        "string",
				Description: "Provider attachment id from the message metadata.",
			},
			"dest_dir": {
				Type:        "string",
				Description: "Optional destination directory for the saved file. Defaults to ~/Downloads/sloppy-attachments. Created if missing. Supports a leading ~/.",
			},
		},
	},
	{
		Name:        "mail_action",
		Description: "Apply one mailbox action to one or more messages, optionally resolving targets from a search query.",
		Required:    []string{"account_id", "action"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"action": {
				Type:        "string",
				Description: "Mailbox action.",
				Enum:        []string{"mark_read", "mark_unread", "archive", "move_to_inbox", "trash", "delete", "defer", "move_to_folder", "apply_label", "archive_label"},
			},
			"message_id": {
				Type:        "string",
				Description: "Optional single provider message id. Use message ids or query.",
			},
			"message_ids": {
				Type:        "array",
				Description: "Optional list of provider message ids. Use message ids or query.",
			},
			"query": {
				Type:        "string",
				Description: "Optional free-text search query used to resolve target messages before applying the action.",
			},
			"limit": {
				Type:        "integer",
				Description: "Optional maximum number of messages to resolve when query is used.",
			},
			"folder": {
				Type:        "string",
				Description: "Required for move_to_folder.",
			},
			"label": {
				Type:        "string",
				Description: "Required for apply_label or archive_label.",
			},
			"archive": {
				Type:        "boolean",
				Description: "Optional archive hint for apply_label.",
			},
			"until": {
				Type:        "string",
				Description: "Required for defer. Accepts RFC3339, YYYY-MM-DDTHH:MM, YYYY-MM-DD HH:MM, or YYYY-MM-DD.",
			},
		},
	},
	{
		Name:        "mail_message_copy",
		Description: "Copy one or more messages from one mail account to another, preserving full message content including attachments.",
		Required:    []string{"source_account_id", "target_account_id", "target_folder"},
		Properties: map[string]ToolProperty{
			"source_account_id": {
				Type:        "integer",
				Description: "External account id of the source mail account.",
			},
			"target_account_id": {
				Type:        "integer",
				Description: "External account id of the target mail account.",
			},
			"target_folder": {
				Type:        "string",
				Description: "Target folder or label name in the destination account.",
			},
			"message_id": {
				Type:        "string",
				Description: "Optional single provider message id. Use message_id or message_ids.",
			},
			"message_ids": {
				Type:        "array",
				Description: "Optional list of provider message ids. Use message_id or message_ids.",
			},
		},
	},
	{
		Name:        "mail_send",
		Description: "Compose and send (or save as draft) a plain-text email from a mail account, with optional attachments. Supports explicit In-Reply-To and References headers when you want to stitch threads manually.",
		Required:    []string{"account_id", "to", "subject", "body"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id to send from.",
			},
			"to": {
				Type:        "array",
				Description: "Recipient email addresses. Accepts plain addresses or Name <addr@example.com> items.",
			},
			"cc": {
				Type:        "array",
				Description: "Optional Cc addresses.",
			},
			"bcc": {
				Type:        "array",
				Description: "Optional Bcc addresses.",
			},
			"subject": {
				Type:        "string",
				Description: "Message subject.",
			},
			"body": {
				Type:        "string",
				Description: "Plain-text body. Newlines are preserved; the message is always sent text/plain UTF-8.",
			},
			"in_reply_to": {
				Type:        "string",
				Description: "Optional In-Reply-To header value (message-id to reply to).",
			},
			"references": {
				Type:        "array",
				Description: "Optional References header values (list of message-ids for thread context).",
			},
			"attachments": {
				Type:        "array",
				Description: "Optional list of attachments. Each item must be an object with filename, content_base64, and optional content_type, OR an object with path to load the file from disk.",
			},
			"draft_only": {
				Type:        "boolean",
				Description: "When true, save as draft without sending. Default false.",
			},
		},
	},
	{
		Name:        "mail_draft_send",
		Description: "Send an existing draft by id without rewriting its content. Supported for accounts whose backend can dispatch a saved draft directly (Exchange EWS, Gmail). Use this after mail_send with draft_only=true, or to send a draft you edited in your mail client.",
		Required:    []string{"account_id", "draft_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id that owns the draft.",
			},
			"draft_id": {
				Type:        "string",
				Description: "Provider draft id returned by mail_send (or visible on the draft in the mail store).",
			},
		},
	},
	{
		Name:        "mail_reply",
		Description: "Reply to an existing message with the correct In-Reply-To/References threading and a properly formatted plain-text quote. Supports two quote styles: bottom_post (GCC / mailing-list interleaved: quote above, reply below) and top_post (business: reply above, quote below).",
		Required:    []string{"account_id", "message_id", "body"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id to reply from.",
			},
			"message_id": {
				Type:        "string",
				Description: "Provider message id of the message being replied to.",
			},
			"body": {
				Type:        "string",
				Description: "Plain-text body of the new reply, before quoting.",
			},
			"quote_style": {
				Type:        "string",
				Description: "Quote placement: bottom_post (GCC / mailing-list; quote above, reply below) or top_post (modern business; reply above, quote below). Default bottom_post.",
				Enum:        []string{"bottom_post", "top_post"},
			},
			"reply_all": {
				Type:        "boolean",
				Description: "When true, also include the original Cc recipients. Default false.",
			},
			"to": {
				Type:        "array",
				Description: "Optional override of To recipients; by default the original From address is used.",
			},
			"cc": {
				Type:        "array",
				Description: "Optional Cc recipients; merged with original Cc when reply_all is true.",
			},
			"bcc": {
				Type:        "array",
				Description: "Optional Bcc recipients.",
			},
			"subject": {
				Type:        "string",
				Description: "Optional subject override. Defaults to Re: <original subject>.",
			},
			"attachments": {
				Type:        "array",
				Description: "Optional list of attachments. Each item must be an object with filename, content_base64, and optional content_type, OR an object with path to load the file from disk.",
			},
			"draft_only": {
				Type:        "boolean",
				Description: "When true, save as draft without sending. Default false.",
			},
		},
	},
	{
		Name:        "mail_server_filter_list",
		Description: "List provider-native server filters or rules when supported.",
		Required:    []string{"account_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
		},
	},
	{
		Name:        "mail_server_filter_upsert",
		Description: "Create or update a provider-native server filter when supported.",
		Required:    []string{"account_id", "filter"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"filter_id": {
				Type:        "string",
				Description: "Optional override filter id for updates.",
			},
			"filter": {
				Type:        "object",
				Description: "Server filter payload.",
			},
		},
	},
	{
		Name:        "mail_server_filter_delete",
		Description: "Delete a provider-native server filter when supported.",
		Required:    []string{"account_id", "filter_id"},
		Properties: map[string]ToolProperty{
			"account_id": {
				Type:        "integer",
				Description: "External account id.",
			},
			"filter_id": {
				Type:        "string",
				Description: "Provider filter id.",
			},
		},
	},
}

var MCPDaemonRoutes = []string{
	"POST /mcp",
	"GET /mcp",
	"DELETE /mcp",
	"GET /ws/canvas",
	"GET /files/*",
	"GET /health",
}

func MCPToolNamesCSV() string {
	names := make([]string, 0, len(MCPTools))
	for _, tool := range MCPTools {
		names = append(names, "`"+tool.Name+"`")
	}
	return strings.Join(names, ", ")
}
