package mcp

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
	case "task_list_list", "task_list_create", "task_list_delete", "task_list", "task_get", "task_create", "task_update", "task_complete", "task_delete":
		return handledTool(s.dispatchTasks(name, args))
	case "evernote_notebook_list", "evernote_note_search", "evernote_note_get":
		return handledTool(s.dispatchEvernote(name, args))
	case "brain.config.get", "brain.vault.list", "brain.note.parse", "brain.note.validate", "brain.note.write", "brain.vault.validate", "brain.links.resolve", "brain.folder.parse", "brain.folder.validate", "brain.folder.links", "brain.folder.audit", "brain.glossary.parse", "brain.glossary.validate", "brain.attention.parse", "brain.attention.validate", "brain.entities.candidates", "brain.gtd.parse", "brain.gtd.list", "brain.projects.render", "brain.projects.list", "brain.gtd.write", "brain.gtd.bulk_link", "brain.gtd.organize", "brain.gtd.resurface", "brain.gtd.dashboard", "brain.gtd.review_batch", "brain.gtd.ingest", "brain.search", "brain.backlinks", "brain_search", "brain_backlinks", "brain.gtd.bind", "brain.gtd.dedup_scan", "brain.gtd.dedup_review_apply", "brain.gtd.dedup_history", "brain.gtd.review_list", "brain.gtd.set_status", "brain.gtd.sync", "brain.people.dashboard", "brain.people.render":
		return handledTool(s.dispatchBrain(name, args))
	case "meeting.summary.draft", "meeting.summary.send", "meeting.share.create", "meeting.share.revoke":
		return handledTool(s.dispatchMeetingTool(name, args))
	default:
		return unhandledTool()
	}
}
