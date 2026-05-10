package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/store"
)

func (s *Server) callConsolidatedTool(name string, args map[string]interface{}) toolDispatchResult {
	action := strings.TrimSpace(strArg(args, "action"))
	sid := strArg(args, "session_id")
	switch name {
	case "sloppy_mail":
		switch action {
		case "account_list":
			return handledTool(s.mailAccountList(args))
		case "label_list":
			return handledTool(s.mailLabelList(args))
		case "message_list":
			return handledTool(s.mailMessageList(args))
		case "message_get":
			return handledTool(s.mailMessageGet(args))
		case "attachment_get":
			return handledTool(s.mailAttachmentGet(args))
		case "send":
			return handledTool(s.mailSend(args))
		case "draft_send":
			return handledTool(s.mailDraftSend(args))
		case "reply":
			return handledTool(s.mailReply(args))
		case "mail_action":
			return handledTool(s.mailAction(args))
		case "message_copy":
			return handledTool(s.mailMessageCopy(args))
		case "flag_set":
			return handledTool(s.mailFlagSet(args))
		case "flag_clear":
			return handledTool(s.mailFlagClear(args))
		case "categories_set":
			return handledTool(s.mailCategoriesSet(args))
		case "server_filter_list":
			return handledTool(s.mailServerFilterList(args))
		case "server_filter_upsert":
			return handledTool(s.mailServerFilterUpsert(args))
		case "server_filter_delete":
			return handledTool(s.mailServerFilterDelete(args))
		case "oof_get":
			return handledTool(s.callMailboxSettingsTool("mail_oof_get", args))
		case "oof_set":
			return handledTool(s.callMailboxSettingsTool("mail_oof_set", args))
		case "delegate_list":
			return handledTool(s.callMailboxSettingsTool("mail_delegate_list", args))
		case "commitment_list":
			return handledTool(s.mailCommitmentList(args))
		case "commitment_close":
			return handledTool(s.mailCommitmentClose(args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_mail: unknown action %q", action))
		}
	case "sloppy_calendar":
		switch action {
		case "list":
			return handledTool(s.calendarList(args))
		case "events":
			return handledTool(s.calendarEvents(args))
		case "event_create":
			return handledTool(s.calendarEventCreate(args))
		case "freebusy":
			return handledTool(s.calendarFreeBusy(args))
		case "event_get":
			return handledTool(s.dispatchCalendarEvent("calendar_event_get", args))
		case "event_update":
			return handledTool(s.dispatchCalendarEvent("calendar_event_update", args))
		case "event_delete":
			return handledTool(s.dispatchCalendarEvent("calendar_event_delete", args))
		case "event_respond":
			return handledTool(s.dispatchCalendarEvent("calendar_event_respond", args))
		case "event_ics_export":
			return handledTool(s.dispatchCalendarEvent("calendar_event_ics_export", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_calendar: unknown action %q", action))
		}
	case "sloppy_tasks":
		switch action {
		case "list_lists":
			return handledTool(s.dispatchTasks("task_list_list", args))
		case "list_create":
			return handledTool(s.dispatchTasks("task_list_create", args))
		case "list_delete":
			return handledTool(s.dispatchTasks("task_list_delete", args))
		case "list":
			return handledTool(s.dispatchTasks("task_list", args))
		case "get":
			return handledTool(s.dispatchTasks("task_get", args))
		case "create":
			return handledTool(s.dispatchTasks("task_create", args))
		case "update":
			return handledTool(s.dispatchTasks("task_update", args))
		case "complete":
			return handledTool(s.dispatchTasks("task_complete", args))
		case "delete":
			return handledTool(s.dispatchTasks("task_delete", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_tasks: unknown action %q", action))
		}
	case "sloppy_contacts":
		switch action {
		case "list":
			return handledTool(s.contactList(args))
		case "get":
			return handledTool(s.contactGet(args))
		case "search":
			return handledTool(s.contactSearch(args))
		case "create":
			return handledTool(s.contactCreate(args))
		case "update":
			return handledTool(s.contactUpdate(args))
		case "delete":
			return handledTool(s.contactDelete(args))
		case "group_list":
			return handledTool(s.contactGroupList(args))
		case "photo_get":
			return handledTool(s.contactPhotoGet(args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_contacts: unknown action %q", action))
		}
	case "sloppy_brain":
		brainMethod := brainActionToMethod(action)
		if brainMethod == "" {
			return handledTool(nil, fmt.Errorf("sloppy_brain: unknown action %q", action))
		}
		return handledTool(s.dispatchBrain(brainMethod, args))
	case "sloppy_workspace":
		switch action {
		case "list":
			return handledTool(s.workspaceList(args))
		case "activate":
			return handledTool(s.workspaceActivate(args))
		case "get":
			return handledTool(s.workspaceGet(args))
		case "watch_start":
			return handledTool(s.workspaceWatchStart(args))
		case "watch_stop":
			return handledTool(s.workspaceWatchStop(args))
		case "watch_status":
			return handledTool(s.workspaceWatchStatus(args))
		case "item_list":
			return handledTool(s.itemList(args))
		case "item_get":
			return handledTool(s.itemGet(args))
		case "item_create":
			return handledTool(s.itemCreate(args))
		case "item_triage":
			return handledTool(s.itemTriage(args))
		case "item_assign":
			return handledTool(s.itemAssign(args))
		case "item_update":
			return handledTool(s.itemUpdate(args))
		case "artifact_get":
			return handledTool(s.artifactGet(args))
		case "artifact_list":
			return handledTool(s.artifactList(args))
		case "actor_list":
			return handledTool(s.actorList(args))
		case "actor_create":
			return handledTool(s.actorCreate(args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_workspace: unknown action %q", action))
		}
	case "sloppy_evernote":
		switch action {
		case "notebook_list":
			return handledTool(s.dispatchEvernote("evernote_notebook_list", args))
		case "note_search":
			return handledTool(s.dispatchEvernote("evernote_note_search", args))
		case "note_get":
			return handledTool(s.dispatchEvernote("evernote_note_get", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_evernote: unknown action %q", action))
		}
	case "sloppy_inbox":
		switch action {
		case "source_list":
			return handledTool(s.dispatchInbox("inbox.source_list", args))
		case "item_list":
			return handledTool(s.dispatchInbox("inbox.item_list", args))
		case "item_plan":
			return handledTool(s.dispatchInbox("inbox.item_plan", args))
		case "item_ack":
			return handledTool(s.dispatchInbox("inbox.item_ack", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_inbox: unknown action %q", action))
		}
	case "sloppy_meeting":
		switch action {
		case "summary_draft":
			return handledTool(s.dispatchMeetingTool("meeting.summary.draft", args))
		case "summary_send":
			return handledTool(s.dispatchMeetingTool("meeting.summary.send", args))
		case "share_create":
			return handledTool(s.dispatchMeetingTool("meeting.share.create", args))
		case "share_revoke":
			return handledTool(s.dispatchMeetingTool("meeting.share.revoke", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_meeting: unknown action %q", action))
		}
	case "sloppy_canvas":
		switch action {
		case "session_open":
			return handledTool(s.dispatchCanvas(sid, "canvas_session_open", args))
		case "artifact_show":
			return handledTool(s.dispatchCanvas(sid, "canvas_artifact_show", args))
		case "status":
			return handledTool(s.dispatchCanvas(sid, "canvas_status", args))
		case "import_handoff":
			return handledTool(s.dispatchCanvas(sid, "canvas_import_handoff", args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_canvas: unknown action %q", action))
		}
	case "sloppy_handoff":
		switch action {
		case "create":
			return handledTool(s.handoffCreate(args))
		case "peek":
			return handledTool(s.handoffPeek(args))
		case "consume":
			return handledTool(s.handoffConsume(args))
		case "revoke":
			return handledTool(s.handoffRevoke(args))
		case "status":
			return handledTool(s.handoffStatus(args))
		case "temp_create":
			return handledTool(s.tempFileCreate(args))
		case "temp_remove":
			return handledTool(s.tempFileRemove(args))
		default:
			return handledTool(nil, fmt.Errorf("sloppy_handoff: unknown action %q", action))
		}
	case "sloppy_tool_help":
		return handledTool(toolHelpHandler(args))
	default:
		return unhandledTool()
	}
}

type toolDispatchResult struct {
	payload map[string]interface{}
	err     error
	ok      bool
}

func handledTool(payload map[string]interface{}, err error) toolDispatchResult {
	return toolDispatchResult{payload: payload, err: err, ok: true}
}

func unhandledTool() toolDispatchResult {
	return toolDispatchResult{}
}

func firstCapableAccount(st *store.Store, sphere, capability string, isCapable func(string) bool) (store.ExternalAccount, error) {
	accounts, err := st.ListExternalAccounts(strings.TrimSpace(sphere))
	if err != nil {
		return store.ExternalAccount{}, err
	}
	matches := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if !account.Enabled {
			continue
		}
		if !isCapable(account.Provider) {
			continue
		}
		matches = append(matches, account)
	}
	if len(matches) == 0 {
		if sphere != "" {
			return store.ExternalAccount{}, fmt.Errorf("no enabled %s account for sphere %q", capability, sphere)
		}
		return store.ExternalAccount{}, fmt.Errorf("no enabled %s account is configured", capability)
	}
	sort.Slice(matches, func(i, j int) bool {
		if matches[i].Sphere != matches[j].Sphere {
			return matches[i].Sphere < matches[j].Sphere
		}
		if matches[i].Provider != matches[j].Provider {
			return matches[i].Provider < matches[j].Provider
		}
		return matches[i].ID < matches[j].ID
	})
	return matches[0], nil
}

func accountForTool(st *store.Store, args map[string]interface{}, capability string, isCapable func(string) bool) (store.ExternalAccount, error) {
	accountIDPtr, _, err := optionalInt64Arg(args, "account_id")
	if err != nil {
		return store.ExternalAccount{}, err
	}
	if accountIDPtr == nil {
		return firstCapableAccount(st, strings.TrimSpace(strArg(args, "sphere")), capability, isCapable)
	}
	account, err := st.GetExternalAccount(*accountIDPtr)
	if err != nil {
		return store.ExternalAccount{}, err
	}
	if !account.Enabled {
		return store.ExternalAccount{}, fmt.Errorf("account %d is disabled", account.ID)
	}
	if !isCapable(account.Provider) {
		return store.ExternalAccount{}, fmt.Errorf("account %d provider %q does not support %s", account.ID, account.Provider, capability)
	}
	return account, nil
}

func emailCapableProvider(provider string) bool {
	return store.IsEmailProvider(provider)
}

func applyToolSchemaDefaults(name string, schema map[string]interface{}) {
	switch name {
	case "calendar_events":
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			schema["properties"] = props
		}
		props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum events to return. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
		props["days"] = map[string]interface{}{"type": "integer", "description": "Days forward from now. Use 7 for upcoming-week summaries."}
	case "mail_label_list", "mail_message_list", "mail_message_get", "mail_attachment_get", "mail_commitment_list":
		removeRequired(schema, "account_id")
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			schema["properties"] = props
		}
		props["account_id"] = map[string]interface{}{"type": "integer", "description": "Optional external account id. Defaults to the first enabled email account for the sphere."}
		props["sphere"] = map[string]interface{}{"type": "string", "description": "Optional work/private account filter used when account_id is omitted.", "enum": []string{"work", "private"}}
		if name == "mail_message_list" {
			props["folder"] = map[string]interface{}{"type": "string", "description": "Folder or label scope. Use INBOX for recent inbox triage."}
			props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum messages to return. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
			props["include_body"] = map[string]interface{}{"type": "boolean", "description": "Include full message bodies. Defaults to false; prefer mail_message_get for one chosen message."}
		}
		if name == "mail_commitment_list" {
			props["limit"] = map[string]interface{}{"type": "integer", "description": "Maximum messages to inspect. Use 5-10 for triage/counts; only request more when the user asks for breadth."}
			props["body_limit"] = map[string]interface{}{"type": "integer", "description": "Maximum number of matching messages whose full bodies may be fetched to confirm a commitment. Defaults to 5."}
			props["project_config"] = map[string]interface{}{"type": "string", "description": "Optional path to per-user project matching rules."}
			props["vault_config"] = map[string]interface{}{"type": "string", "description": "Optional vault config path used for person-note diagnostics."}
			props["writeable"] = map[string]interface{}{"type": "boolean", "description": "When true, returned source bindings opt into upstream sync-back."}
		}
	case "mail_commitment_close":
		props, _ := schema["properties"].(map[string]interface{})
		if props == nil {
			props = map[string]interface{}{}
			schema["properties"] = props
		}
		props["writeable"] = map[string]interface{}{"type": "boolean", "description": "Must be true, copied from the source binding."}
		props["action"] = map[string]interface{}{"type": "string", "description": "Mail action to apply. Defaults to archive."}
	}
}

func applyToolDefinitionDefaults(name string, def map[string]interface{}) {
	switch name {
	case "calendar_events":
		def["description"] = "List upcoming personal/work groupware calendar events. Compact by default: descriptions and attendee lists are omitted; use sphere plus limit 5-10 for triage/counts."
	case "mail_message_list":
		def["description"] = "List newest mail metadata without full bodies by default. Prefer sphere plus folder=INBOX and limit 5-10 for triage/counts; use mail_message_get for one chosen message body."
	case "mail_commitment_list":
		def["description"] = "Derive GTD commitments from mail messages without fetching every body. Prefer sphere plus limit 5-10 for triage/counts; use body_limit to bound confirmation fetches."
	case "mail_commitment_close":
		def["description"] = "Close a writeable mail-bound commitment by applying an upstream mail action. Requires writeable=true from the source binding."
	}
}

func removeRequired(schema map[string]interface{}, field string) {
	required, _ := schema["required"].([]string)
	if len(required) == 0 {
		return
	}
	filtered := required[:0]
	for _, item := range required {
		if item != field {
			filtered = append(filtered, item)
		}
	}
	if len(filtered) == 0 {
		delete(schema, "required")
		return
	}
	schema["required"] = filtered
}

