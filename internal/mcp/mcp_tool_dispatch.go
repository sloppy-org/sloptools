package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

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

func (s *Server) callCanvasTool(sid, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "canvas_session_open", "canvas_activate", "canvas_artifact_show", "canvas_render_text", "canvas_render_image", "canvas_render_pdf", "canvas_clear", "canvas_status", "canvas_history", "canvas_import_handoff":
		return handledTool(s.dispatchCanvas(sid, name, args))
	default:
		return unhandledTool()
	}
}

func (s *Server) callCoreTool(_, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "handoff.create":
		return handledTool(s.handoffCreate(args))
	case "handoff.peek":
		return handledTool(s.handoffPeek(args))
	case "handoff.consume":
		return handledTool(s.handoffConsume(args))
	case "handoff.revoke":
		return handledTool(s.handoffRevoke(args))
	case "handoff.status":
		return handledTool(s.handoffStatus(args))
	case "temp_file_create":
		return handledTool(s.tempFileCreate(args))
	case "temp_file_remove":
		return handledTool(s.tempFileRemove(args))
	case "workspace_list":
		return handledTool(s.workspaceList(args))
	case "workspace_activate":
		return handledTool(s.workspaceActivate(args))
	case "workspace_get":
		return handledTool(s.workspaceGet(args))
	case "workspace_watch_start":
		return handledTool(s.workspaceWatchStart(args))
	case "workspace_watch_stop":
		return handledTool(s.workspaceWatchStop(args))
	case "workspace_watch_status":
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
		return unhandledTool()
	}
}

func (s *Server) callCalendarTool(_, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "calendar_list":
		return handledTool(s.calendarList(args))
	case "calendar_events":
		return handledTool(s.calendarEvents(args))
	case "calendar_event_create":
		return handledTool(s.calendarEventCreate(args))
	case "calendar_freebusy":
		return handledTool(s.calendarFreeBusy(args))
	case "calendar_event_get", "calendar_event_update", "calendar_event_delete", "calendar_event_respond", "calendar_event_ics_export":
		return handledTool(s.dispatchCalendarEvent(name, args))
	default:
		return unhandledTool()
	}
}

func (s *Server) callMailTool(_, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "mail_account_list":
		return handledTool(s.mailAccountList(args))
	case "mail_label_list":
		return handledTool(s.mailLabelList(args))
	case "mail_message_list":
		return handledTool(s.mailMessageList(args))
	case "mail_message_get":
		return handledTool(s.mailMessageGet(args))
	case "mail_commitment_list":
		return handledTool(s.mailCommitmentList(args))
	case "mail_commitment_close":
		return handledTool(s.mailCommitmentClose(args))
	case "mail_attachment_get":
		return handledTool(s.mailAttachmentGet(args))
	case "mail_action":
		return handledTool(s.mailAction(args))
	case "mail_send":
		return handledTool(s.mailSend(args))
	case "mail_draft_send":
		return handledTool(s.mailDraftSend(args))
	case "mail_reply":
		return handledTool(s.mailReply(args))
	case "mail_message_copy":
		return handledTool(s.mailMessageCopy(args))
	case "mail_server_filter_list":
		return handledTool(s.mailServerFilterList(args))
	case "mail_server_filter_upsert":
		return handledTool(s.mailServerFilterUpsert(args))
	case "mail_server_filter_delete":
		return handledTool(s.mailServerFilterDelete(args))
	case "mail_flag_set":
		return handledTool(s.mailFlagSet(args))
	case "mail_flag_clear":
		return handledTool(s.mailFlagClear(args))
	case "mail_categories_set":
		return handledTool(s.mailCategoriesSet(args))
	case "mail_oof_get", "mail_oof_set", "mail_delegate_list":
		return handledTool(s.callMailboxSettingsTool(name, args))
	default:
		return unhandledTool()
	}
}

func (s *Server) callContactTool(_, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "contact_list":
		return handledTool(s.contactList(args))
	case "contact_get":
		return handledTool(s.contactGet(args))
	case "contact_search":
		return handledTool(s.contactSearch(args))
	case "contact_create":
		return handledTool(s.contactCreate(args))
	case "contact_update":
		return handledTool(s.contactUpdate(args))
	case "contact_delete":
		return handledTool(s.contactDelete(args))
	case "contact_group_list":
		return handledTool(s.contactGroupList(args))
	case "contact_photo_get":
		return handledTool(s.contactPhotoGet(args))
	default:
		return unhandledTool()
	}
}

func (s *Server) callAuxTool(_, name string, args map[string]interface{}) toolDispatchResult {
	switch name {
	case "inbox.source_list", "inbox.item_list", "inbox.item_plan", "inbox.item_ack":
		return handledTool(s.dispatchInbox(name, args))
	case "task_list_list", "task_list_create", "task_list_delete", "task_list", "task_get", "task_create", "task_update", "task_complete", "task_delete":
		return handledTool(s.dispatchTasks(name, args))
	case "evernote_notebook_list", "evernote_note_search", "evernote_note_get":
		return handledTool(s.dispatchEvernote(name, args))
	case "brain.config.get", "brain.vault.list", "brain.note.parse", "brain.note.validate", "brain.note.write", "brain.vault.validate", "brain.links.resolve", "brain.folder.parse", "brain.folder.validate", "brain.folder.links", "brain.folder.audit", "brain.glossary.parse", "brain.glossary.validate", "brain.attention.parse", "brain.attention.validate", "brain.entities.candidates", "brain.gtd.parse", "brain.gtd.list", "brain.gtd.tracks", "brain.gtd.focus", "brain.projects.render", "brain.projects.list", "brain.gtd.write", "brain.gtd.bulk_link", "brain.gtd.organize", "brain.gtd.resurface", "brain.gtd.dashboard", "brain.gtd.today", "brain.gtd.review_batch", "brain.gtd.ingest", "brain.search", "brain.backlinks", "brain_search", "brain_backlinks", "brain.gtd.bind", "brain.gtd.dedup_scan", "brain.gtd.dedup_review_apply", "brain.gtd.dedup_history", "brain.gtd.review_list", "brain.gtd.set_status", "brain.gtd.sync", "brain.people.dashboard", "brain.people.render", "brain.people.brief", "brain.people.monthly_index", "brain.meeting.kickoff":
		return handledTool(s.dispatchBrain(name, args))
	case "meeting.summary.draft", "meeting.summary.send", "meeting.share.create", "meeting.share.revoke":
		return handledTool(s.dispatchMeetingTool(name, args))
	default:
		return unhandledTool()
	}
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

type affectedRef struct {
	Domain      string `json:"domain"`
	Kind        string `json:"kind"`
	Provider    string `json:"provider,omitempty"`
	AccountID   int64  `json:"account_id,omitempty"`
	ID          string `json:"id,omitempty"`
	PreviousID  string `json:"previous_id,omitempty"`
	ContainerID string `json:"container_id,omitempty"`
	Path        string `json:"path,omitempty"`
	Sphere      string `json:"sphere,omitempty"`
}

func withAffected(result map[string]interface{}, refs ...affectedRef) map[string]interface{} {
	compact := compactAffectedRefs(refs...)
	if len(compact) > 0 {
		result["affected"] = compact
	}
	return result
}

func compactAffectedRefs(refs ...affectedRef) []affectedRef {
	out := make([]affectedRef, 0, len(refs))
	seen := map[string]struct{}{}
	for _, ref := range refs {
		ref.Domain = strings.TrimSpace(ref.Domain)
		ref.Kind = strings.TrimSpace(ref.Kind)
		ref.Provider = strings.TrimSpace(ref.Provider)
		ref.ID = strings.TrimSpace(ref.ID)
		ref.PreviousID = strings.TrimSpace(ref.PreviousID)
		ref.ContainerID = strings.TrimSpace(ref.ContainerID)
		ref.Path = strings.TrimSpace(ref.Path)
		ref.Sphere = strings.TrimSpace(ref.Sphere)
		if ref.Kind == "" || (ref.ID == "" && ref.Path == "") {
			continue
		}
		key := strings.Join([]string{
			ref.Domain,
			ref.Kind,
			ref.Provider,
			ref.Sphere,
			ref.Path,
			ref.ContainerID,
			ref.PreviousID,
			ref.ID,
		}, "\x00")
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, ref)
	}
	return out
}

func brainCommitmentAffectedRef(sphere, path string) affectedRef {
	return affectedRef{
		Domain:   "brain",
		Kind:     "gtd_commitment",
		Provider: "markdown",
		ID:       path,
		Path:     path,
		Sphere:   sphere,
	}
}

func brainCommitmentAffectedRefs(sphere string, paths []string) []affectedRef {
	refs := make([]affectedRef, 0, len(paths))
	for _, path := range paths {
		refs = append(refs, brainCommitmentAffectedRef(sphere, path))
	}
	return compactAffectedRefs(refs...)
}

func mailMessageAffectedRefs(account store.ExternalAccount, messageIDs []string, resolutions []email.ActionResolution) []affectedRef {
	refs := make([]affectedRef, 0, len(messageIDs)+len(resolutions))
	provider := strings.TrimSpace(account.Provider)
	skipIDs := map[string]struct{}{}
	for _, resolution := range resolutions {
		id := strings.TrimSpace(resolution.ResolvedMessageID)
		if id == "" {
			id = strings.TrimSpace(resolution.OriginalMessageID)
		}
		if id != "" {
			skipIDs[id] = struct{}{}
		}
		if original := strings.TrimSpace(resolution.OriginalMessageID); original != "" {
			skipIDs[original] = struct{}{}
		}
		refs = append(refs, affectedRef{
			Domain:     "mail",
			Kind:       "message",
			Provider:   provider,
			AccountID:  account.ID,
			ID:         id,
			PreviousID: resolution.OriginalMessageID,
		})
	}
	for _, messageID := range messageIDs {
		if _, ok := skipIDs[strings.TrimSpace(messageID)]; ok {
			continue
		}
		refs = append(refs, affectedRef{
			Domain:    "mail",
			Kind:      "message",
			Provider:  provider,
			AccountID: account.ID,
			ID:        messageID,
		})
	}
	return compactAffectedRefs(refs...)
}

func taskAffectedRef(account store.ExternalAccount, providerName string, item providerdata.TaskItem) affectedRef {
	id := strings.TrimSpace(item.ID)
	if id == "" {
		id = strings.TrimSpace(item.ProviderRef)
	}
	return affectedRef{
		Domain:      "tasks",
		Kind:        "task",
		Provider:    strings.TrimSpace(providerName),
		AccountID:   account.ID,
		ID:          id,
		ContainerID: strings.TrimSpace(item.ListID),
	}
}

func taskAffectedRefByID(account store.ExternalAccount, providerName, listID, id string) affectedRef {
	return affectedRef{
		Domain:      "tasks",
		Kind:        "task",
		Provider:    strings.TrimSpace(providerName),
		AccountID:   account.ID,
		ID:          strings.TrimSpace(id),
		ContainerID: strings.TrimSpace(listID),
	}
}

func calendarEventAffectedRef(account store.ExternalAccount, providerName, sphere, calendarID, eventID string) affectedRef {
	return affectedRef{
		Domain:      "calendar",
		Kind:        "event",
		Provider:    strings.TrimSpace(providerName),
		AccountID:   account.ID,
		ID:          strings.TrimSpace(eventID),
		ContainerID: strings.TrimSpace(calendarID),
		Sphere:      strings.TrimSpace(sphere),
	}
}

func calendarEventAffectedRefFromEvent(account store.ExternalAccount, providerName, sphere string, event providerdata.Event) affectedRef {
	return calendarEventAffectedRef(account, providerName, sphere, event.CalendarID, event.ID)
}

func gtdSyncAffectedRefs(sphere string, actions []gtdSyncAction) []affectedRef {
	paths := make([]string, 0, len(actions))
	for _, action := range actions {
		path := strings.TrimSpace(action.Path)
		switch action.Action {
		case "", "manual_noop", "upstream_already_closed":
			continue
		}
		if path == "" || action.DryRun {
			continue
		}
		paths = append(paths, path)
	}
	return brainCommitmentAffectedRefs(sphere, paths)
}
