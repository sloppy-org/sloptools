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
	{Name: "sloppy_mail", Description: "Mail. Actions: message listing/reading, draft (save to Drafts folder), send (send immediately), draft_send (send pre-saved draft by draft_id), reply, flag, filter, OOF, commitments. Use sloppy_tool_help tool=mail for the full action list.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":      {Type: "string", Description: "Mail operation."},
		"account_id":  {Type: "integer", Description: "Optional email account id. Defaults to first enabled account for sphere."},
		"sphere":      {Type: "string", Description: "work or private sphere when account_id is omitted.", Enum: []string{"work", "private"}},
		"to":          {Type: "string", Description: "Recipient address(es), comma-separated. Required for draft/send/reply."},
		"cc":          {Type: "string", Description: "CC address(es), comma-separated. Optional for draft/send/reply."},
		"bcc":         {Type: "string", Description: "BCC address(es), comma-separated. Optional for draft/send/reply."},
		"subject":     {Type: "string", Description: "Subject line. Required for draft/send."},
		"body":        {Type: "string", Description: "Plain-text body. Required for draft/send/reply."},
		"in_reply_to": {Type: "string", Description: "Message-ID to thread under. Optional for draft/send."},
		"draft_only":  {Type: "boolean", Description: "When true on send, saves to Drafts without sending. Prefer action=draft instead."},
		"draft_id":    {Type: "string", Description: "Draft ID returned by draft/send with draft_only. Required for draft_send."},
		"message_id":  {Type: "string", Description: "Provider message id. Required for message_get, reply."},
		"folder":      {Type: "string", Description: "Folder or label scope for message_list (e.g. INBOX)."},
		"query":       {Type: "string", Description: "Search query for message_list."},
		"limit":       {Type: "integer", Description: "Maximum messages to return for message_list."},
	}},
	{Name: "sloppy_calendar", Description: "Calendar. Action: list, events, event_create, freebusy, event_get, event_update, event_delete, event_respond, event_ics_export.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":     {Type: "string", Description: "Calendar operation."},
		"account_id": {Type: "integer", Description: "Optional calendar account id."},
		"sphere":     {Type: "string", Description: "work or private sphere.", Enum: []string{"work", "private"}},
	}},
	{Name: "sloppy_tasks", Description: "Tasks. Action: list_lists, list_create, list_delete, list, get, create, update, complete, delete.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":     {Type: "string", Description: "Tasks operation."},
		"account_id": {Type: "integer", Description: "Optional tasks account id."},
		"sphere":     {Type: "string", Description: "work or private sphere.", Enum: []string{"work", "private"}},
	}},
	{Name: "sloppy_contacts", Description: "Contacts. Action: list, get, search, create, update, delete, group_list, photo_get.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":     {Type: "string", Description: "Contacts operation."},
		"account_id": {Type: "integer", Description: "Optional contacts account id."},
		"sphere":     {Type: "string", Description: "work or private sphere.", Enum: []string{"work", "private"}},
	}},
	{Name: "sloppy_brain", Description: "Brain/GTD vault. Key actions: note_parse, note_write, gtd_write, gtd_list, gtd_focus, gtd_sync, people_brief, people_render, search. Use sloppy_tool_help tool=brain for the full list.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":      {Type: "string", Description: "Brain/GTD operation."},
		"sphere":      {Type: "string", Description: "work or private vault.", Enum: []string{"work", "private"}},
		"config_path": {Type: "string", Description: "Optional vault config path."},
	}},
	{Name: "sloppy_workspace", Description: "Workspace, items, actors. Action: list, activate, get, watch_start, watch_stop, watch_status, item_list, item_get, item_create, item_triage, item_assign, item_update, artifact_get, artifact_list, actor_list, actor_create.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action": {Type: "string", Description: "Workspace/items operation."},
		"sphere": {Type: "string", Description: "work or private sphere.", Enum: []string{"work", "private"}},
	}},
	{Name: "sloppy_evernote", Description: "Evernote. Action: notebook_list, note_search, note_get.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":     {Type: "string", Description: "Evernote operation."},
		"account_id": {Type: "integer", Description: "Optional Evernote account id."},
	}},
	{Name: "sloppy_inbox", Description: "Inbox capture. Action: source_list, item_list, item_plan, item_ack.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action": {Type: "string", Description: "Inbox operation."},
		"sphere": {Type: "string", Description: "work or private sphere.", Enum: []string{"work", "private"}},
	}},
	{Name: "sloppy_meeting", Description: "Meeting summaries. Action: summary_draft, summary_send, share_create, share_revoke.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action": {Type: "string", Description: "Meeting operation."},
	}},
	{Name: "sloppy_canvas", Description: "Canvas. Action: session_open, artifact_show, status, import_handoff.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":     {Type: "string", Description: "Canvas operation."},
		"session_id": {Type: "string", Description: "Canvas session id."},
	}},
	{Name: "sloppy_handoff", Description: "Handoffs and temp files. Action: create, peek, consume, revoke, status, temp_create, temp_remove.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action": {Type: "string", Description: "Handoff/temp operation."},
	}},
	{Name: "sloppy_source", Description: "Read-only GitHub source evidence via gh. Actions: github_issue_view, github_pr_view, github_issue_search, github_pr_search, github_code_search.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action": {Type: "string", Description: "Source operation.", Enum: []string{"github_issue_view", "github_pr_view", "github_issue_search", "github_pr_search", "github_code_search"}},
		"repo":   {Type: "string", Description: "GitHub repo as owner/name. Required so scout searches the intended project."},
		"number": {Type: "integer", Description: "Issue or pull request number for *_view actions."},
		"query":  {Type: "string", Description: "Search query for *_search actions."},
		"limit":  {Type: "integer", Description: "Maximum search results. Defaults to 5, capped at 20."},
		"state":  {Type: "string", Description: "Issue/PR state for search: open, closed, merged, all.", Enum: []string{"open", "closed", "merged", "all"}},
	}},
	{Name: "sloppy_chat", Description: "Read-only team chat. Actions: provider_list, stream_list, message_list, message_search. Zulip is the first provider and is configured per sphere in sources.toml.", Required: []string{"action"}, Properties: map[string]ToolProperty{
		"action":         {Type: "string", Description: "Chat operation.", Enum: []string{"provider_list", "stream_list", "message_list", "message_search"}},
		"sphere":         {Type: "string", Description: "Sphere. Defaults to work.", Enum: []string{"work", "private"}},
		"sources_config": {Type: "string", Description: "Optional sources.toml path. Defaults to ~/.config/sloptools/sources.toml."},
		"stream":         {Type: "string", Description: "Zulip stream name."},
		"topic":          {Type: "string", Description: "Zulip topic name."},
		"query":          {Type: "string", Description: "Full-text search query for message_search."},
		"after":          {Type: "string", Description: "RFC3339 inclusive lower timestamp bound."},
		"before":         {Type: "string", Description: "RFC3339 exclusive upper timestamp bound."},
		"limit":          {Type: "integer", Description: "Maximum messages or streams to return."},
	}},
	{Name: "sloppy_tool_help", Description: "List actions for a sloppy tool family.", Required: []string{"tool"}, Properties: map[string]ToolProperty{
		"tool": {Type: "string", Description: "Tool family: mail, calendar, tasks, contacts, brain, workspace, evernote, inbox, meeting, canvas, handoff, source, chat.", Enum: []string{"mail", "calendar", "tasks", "contacts", "brain", "workspace", "evernote", "inbox", "meeting", "canvas", "handoff", "source", "chat"}},
	}},
}

var MCPDaemonRoutes = []string{"POST /mcp", "GET /mcp", "DELETE /mcp", "GET /ws/canvas", "GET /files/*", "GET /health"}

func MCPToolNamesCSV() string {
	names := make([]string, 0, len(MCPTools))
	for _, tool := range MCPTools {
		names = append(names, "`"+tool.Name+"`")
	}
	return strings.Join(names, ", ")
}
