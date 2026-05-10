package mcp

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

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

// brainActionToMethod converts a sloppy_brain action string (underscore form)
// to the dot-separated brain dispatch method name.
func brainActionToMethod(action string) string {
	switch action {
	case "config_get":
		return "brain.config.get"
	case "vault_list":
		return "brain.vault.list"
	case "note_parse":
		return "brain.note.parse"
	case "note_validate":
		return "brain.note.validate"
	case "note_write":
		return "brain.note.write"
	case "vault_validate":
		return "brain.vault.validate"
	case "links_resolve":
		return "brain.links.resolve"
	case "folder_parse":
		return "brain.folder.parse"
	case "folder_validate":
		return "brain.folder.validate"
	case "folder_links":
		return "brain.folder.links"
	case "folder_audit":
		return "brain.folder.audit"
	case "glossary_parse":
		return "brain.glossary.parse"
	case "glossary_validate":
		return "brain.glossary.validate"
	case "attention_parse":
		return "brain.attention.parse"
	case "attention_validate":
		return "brain.attention.validate"
	case "entities_candidates":
		return "brain.entities.candidates"
	case "gtd_parse":
		return "brain.gtd.parse"
	case "gtd_list":
		return "brain.gtd.list"
	case "gtd_tracks":
		return "brain.gtd.tracks"
	case "gtd_focus":
		return "brain.gtd.focus"
	case "projects_render":
		return "brain.projects.render"
	case "projects_list":
		return "brain.projects.list"
	case "gtd_write":
		return "brain.gtd.write"
	case "gtd_bulk_link":
		return "brain.gtd.bulk_link"
	case "gtd_organize":
		return "brain.gtd.organize"
	case "gtd_resurface":
		return "brain.gtd.resurface"
	case "gtd_dashboard":
		return "brain.gtd.dashboard"
	case "gtd_today":
		return "brain.gtd.today"
	case "gtd_review_batch":
		return "brain.gtd.review_batch"
	case "gtd_ingest":
		return "brain.gtd.ingest"
	case "search":
		return "brain.search"
	case "backlinks":
		return "brain.backlinks"
	case "gtd_bind":
		return "brain.gtd.bind"
	case "gtd_dedup_scan":
		return "brain.gtd.dedup_scan"
	case "gtd_dedup_review_apply":
		return "brain.gtd.dedup_review_apply"
	case "gtd_dedup_history":
		return "brain.gtd.dedup_history"
	case "gtd_review_list":
		return "brain.gtd.review_list"
	case "gtd_set_status":
		return "brain.gtd.set_status"
	case "gtd_sync":
		return "brain.gtd.sync"
	case "people_dashboard":
		return "brain.people.dashboard"
	case "people_render":
		return "brain.people.render"
	case "people_brief":
		return "brain.people.brief"
	case "people_monthly_index":
		return "brain.people.monthly_index"
	case "meeting_kickoff":
		return "brain.meeting.kickoff"
	default:
		return ""
	}
}

func toolHelpHandler(args map[string]interface{}) (map[string]interface{}, error) {
	tool := strings.TrimSpace(strArg(args, "tool"))
	help := map[string][]string{
		"mail":      {"account_list", "label_list", "message_list", "message_get", "attachment_get", "send", "draft_send", "reply", "mail_action", "message_copy", "flag_set", "flag_clear", "categories_set", "server_filter_list", "server_filter_upsert", "server_filter_delete", "oof_get", "oof_set", "delegate_list", "commitment_list", "commitment_close"},
		"calendar":  {"list", "events", "event_create", "freebusy", "event_get", "event_update", "event_delete", "event_respond", "event_ics_export"},
		"tasks":     {"list_lists", "list_create", "list_delete", "list", "get", "create", "update", "complete", "delete"},
		"contacts":  {"list", "get", "search", "create", "update", "delete", "group_list", "photo_get"},
		"brain":     {"config_get", "vault_list", "note_parse", "note_validate", "note_write", "vault_validate", "links_resolve", "folder_parse", "folder_validate", "folder_links", "folder_audit", "glossary_parse", "glossary_validate", "attention_parse", "attention_validate", "entities_candidates", "gtd_parse", "gtd_list", "gtd_tracks", "gtd_focus", "projects_render", "projects_list", "gtd_write", "gtd_bulk_link", "gtd_organize", "gtd_resurface", "gtd_dashboard", "gtd_today", "gtd_review_batch", "gtd_ingest", "search", "backlinks", "gtd_bind", "gtd_dedup_scan", "gtd_dedup_review_apply", "gtd_dedup_history", "gtd_review_list", "gtd_set_status", "gtd_sync", "people_dashboard", "people_render", "people_brief", "people_monthly_index", "meeting_kickoff"},
		"workspace": {"list", "activate", "get", "watch_start", "watch_stop", "watch_status", "item_list", "item_get", "item_create", "item_triage", "item_assign", "item_update", "artifact_get", "artifact_list", "actor_list", "actor_create"},
		"evernote":  {"notebook_list", "note_search", "note_get"},
		"inbox":     {"source_list", "item_list", "item_plan", "item_ack"},
		"meeting":   {"summary_draft", "summary_send", "share_create", "share_revoke"},
		"canvas":    {"session_open", "artifact_show", "status", "import_handoff"},
		"handoff":   {"create", "peek", "consume", "revoke", "status", "temp_create", "temp_remove"},
	}
	if tool == "" {
		b, _ := json.Marshal(help)
		return map[string]interface{}{"tools": help, "json": string(b)}, nil
	}
	actions, ok := help[tool]
	if !ok {
		return nil, fmt.Errorf("unknown tool family %q", tool)
	}
	return map[string]interface{}{"tool": tool, "actions": actions}, nil
}
