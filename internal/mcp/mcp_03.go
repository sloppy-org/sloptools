package mcp

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/sloppy-org/sloptools/internal/store"
)

// itemListDefaultLimit caps a single item_list response so a workspace
// with thousands of inbox items does not blow the agent context.
const itemListDefaultLimit = 50

func (s *Server) itemList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	filter, err := domainItemFilter(args)
	if err != nil {
		return nil, err
	}
	state := strings.TrimSpace(strArg(args, "state"))
	var items []store.Item
	if state == "" {
		items, err = st.ListItemsFiltered(filter)
	} else {
		items, err = st.ListItemsByStateFiltered(state, filter)
	}
	if err != nil {
		return nil, err
	}
	total := len(items)
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := intArg(args, "limit", itemListDefaultLimit)
	if limit <= 0 {
		limit = itemListDefaultLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	window := items[offset:end]
	out := map[string]interface{}{
		"items":     window,
		"count":     len(window),
		"total":     total,
		"offset":    offset,
		"limit":     limit,
		"truncated": offset > 0 || end < total,
	}
	if end < total {
		out["next_offset"] = end
	}
	return out, nil
}

func (s *Server) itemGet(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	itemID, err := int64Arg(args, "item_id")
	if err != nil {
		return nil, err
	}
	item, err := st.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"item": item}
	if item.WorkspaceID != nil {
		workspace, err := st.GetWorkspace(*item.WorkspaceID)
		if err != nil {
			return nil, err
		}
		result["workspace"] = workspace
	}
	if item.ActorID != nil {
		actor, err := st.GetActor(*item.ActorID)
		if err != nil {
			return nil, err
		}
		result["actor"] = actor
	}
	if item.ArtifactID != nil {
		artifact, err := st.GetArtifact(*item.ArtifactID)
		if err != nil {
			return nil, err
		}
		result["artifact"] = artifact
	}
	artifacts, err := st.ListItemArtifacts(itemID)
	if err != nil {
		return nil, err
	}
	result["artifacts"] = artifacts
	return result, nil
}

func (s *Server) itemCreate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	title := strings.TrimSpace(strArg(args, "title"))
	if title == "" {
		return nil, errors.New("title is required")
	}
	state := strings.TrimSpace(strArg(args, "state"))
	if state == "" {
		state = store.ItemStateInbox
	}
	workspaceID, _, err := optionalInt64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	artifactID, _, err := optionalInt64Arg(args, "artifact_id")
	if err != nil {
		return nil, err
	}
	actorID, _, err := optionalInt64Arg(args, "actor_id")
	if err != nil {
		return nil, err
	}
	if workspaceID != nil && *workspaceID <= 0 {
		workspaceID = nil
	}
	if artifactID != nil && *artifactID <= 0 {
		artifactID = nil
	}
	if actorID != nil && *actorID <= 0 {
		actorID = nil
	}
	sphere, _ := optionalStringArg(args, "sphere")
	visibleAfter, _ := optionalStringArg(args, "visible_after")
	followUpAt, _ := optionalStringArg(args, "follow_up_at")
	source, _ := optionalStringArg(args, "source")
	sourceRef, _ := optionalStringArg(args, "source_ref")
	item, err := st.CreateItem(title, store.ItemOptions{State: state, WorkspaceID: workspaceID, Sphere: sphere, ArtifactID: artifactID, ActorID: actorID, VisibleAfter: visibleAfter, FollowUpAt: followUpAt, Source: source, SourceRef: sourceRef})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"item": item}, nil
}

func (s *Server) itemTriage(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	itemID, err := int64Arg(args, "item_id")
	if err != nil {
		return nil, err
	}
	action := strings.ToLower(strings.TrimSpace(strArg(args, "action")))
	switch action {
	case "done":
		err = st.TriageItemDone(itemID)
	case "later":
		err = st.TriageItemLater(itemID, strings.TrimSpace(strArg(args, "visible_after")))
	case "delegate":
		actorID, actorErr := int64Arg(args, "actor_id")
		if actorErr != nil {
			return nil, actorErr
		}
		err = st.TriageItemDelegate(itemID, actorID)
	case "delete":
		err = st.TriageItemDelete(itemID)
	case "someday":
		err = st.TriageItemSomeday(itemID)
	default:
		return nil, errors.New("action must be one of done, later, delegate, delete, someday")
	}
	if err != nil {
		return nil, err
	}
	if action == "delete" {
		return map[string]interface{}{"deleted": true, "item_id": itemID}, nil
	}
	item, err := st.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"item": item}, nil
}

func (s *Server) itemAssign(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	itemID, err := int64Arg(args, "item_id")
	if err != nil {
		return nil, err
	}
	actorID, err := int64Arg(args, "actor_id")
	if err != nil {
		return nil, err
	}
	if err := st.AssignItem(itemID, actorID); err != nil {
		return nil, err
	}
	item, err := st.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"item": item}, nil
}

func (s *Server) itemUpdate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	itemID, err := int64Arg(args, "item_id")
	if err != nil {
		return nil, err
	}
	var updates store.ItemUpdate
	changed := false
	if title, ok := optionalStringArg(args, "title"); ok {
		updates.Title = title
		changed = true
	}
	if state, ok := optionalStringArg(args, "state"); ok {
		updates.State = state
		changed = true
	}
	if workspaceID, ok, err := optionalInt64Arg(args, "workspace_id"); err != nil {
		return nil, err
	} else if ok {
		updates.WorkspaceID = workspaceID
		changed = true
	}
	if artifactID, ok, err := optionalInt64Arg(args, "artifact_id"); err != nil {
		return nil, err
	} else if ok {
		updates.ArtifactID = artifactID
		changed = true
	}
	if actorID, ok, err := optionalInt64Arg(args, "actor_id"); err != nil {
		return nil, err
	} else if ok {
		updates.ActorID = actorID
		changed = true
	}
	if sphere, ok := optionalStringArg(args, "sphere"); ok {
		updates.Sphere = sphere
		changed = true
	}
	if visibleAfter, ok := optionalStringArg(args, "visible_after"); ok {
		updates.VisibleAfter = visibleAfter
		changed = true
	}
	if followUpAt, ok := optionalStringArg(args, "follow_up_at"); ok {
		updates.FollowUpAt = followUpAt
		changed = true
	}
	if source, ok := optionalStringArg(args, "source"); ok {
		updates.Source = source
		changed = true
	}
	if sourceRef, ok := optionalStringArg(args, "source_ref"); ok {
		updates.SourceRef = sourceRef
		changed = true
	}
	if !changed {
		return nil, errors.New("at least one item update is required")
	}
	if err := st.UpdateItem(itemID, updates); err != nil {
		return nil, err
	}
	item, err := st.GetItem(itemID)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"item": item}, nil
}

func (s *Server) artifactGet(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	artifactID, err := int64Arg(args, "artifact_id")
	if err != nil {
		return nil, err
	}
	artifact, err := st.GetArtifact(artifactID)
	if err != nil {
		return nil, err
	}
	items, err := st.ListArtifactItems(artifactID)
	if err != nil {
		return nil, err
	}
	result := map[string]interface{}{"artifact": artifact, "items": items}
	if content, reason := s.readArtifactContent(artifact); reason == "" {
		result["content_text"] = content
	} else {
		result["content_unavailable_reason"] = reason
	}
	return result, nil
}

func (s *Server) artifactList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	kind := store.ArtifactKind(strings.TrimSpace(strArg(args, "kind")))
	workspaceID, hasWorkspace, err := optionalInt64Arg(args, "workspace_id")
	if err != nil {
		return nil, err
	}
	linkedOnly := boolArg(args, "linked_only")
	var artifacts []store.Artifact
	switch {
	case hasWorkspace && workspaceID != nil && *workspaceID > 0:
		if linkedOnly {
			artifacts, err = st.ListLinkedArtifacts(*workspaceID)
		} else {
			artifacts, err = st.ListArtifactsForWorkspace(*workspaceID)
		}
	case kind != "":
		artifacts, err = st.ListArtifactsByKind(kind)
	default:
		artifacts, err = st.ListArtifacts()
	}
	if err != nil {
		return nil, err
	}
	if limit := intArg(args, "limit", 0); limit > 0 && len(artifacts) > limit {
		artifacts = artifacts[:limit]
	}
	return map[string]interface{}{"artifacts": artifacts, "count": len(artifacts)}, nil
}

func (s *Server) actorList(_ map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	actors, err := st.ListActors()
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"actors": actors, "count": len(actors)}, nil
}

func (s *Server) actorCreate(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	name := strings.TrimSpace(strArg(args, "name"))
	kind := strings.TrimSpace(strArg(args, "kind"))
	if name == "" {
		return nil, errors.New("name is required")
	}
	if kind == "" {
		return nil, errors.New("kind is required")
	}
	actor, err := st.CreateActor(name, kind)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"actor": actor}, nil
}

func (s *Server) readArtifactContent(artifact store.Artifact) (string, string) {
	if artifact.RefPath == nil || strings.TrimSpace(*artifact.RefPath) == "" {
		return "", "artifact has no local ref_path"
	}
	projectDir := strings.TrimSpace(s.projectDir)
	if projectDir == "" {
		return "", "project dir unavailable"
	}
	target := strings.TrimSpace(*artifact.RefPath)
	var absPath string
	if filepath.IsAbs(target) {
		absPath = filepath.Clean(target)
	} else {
		absPath = filepath.Clean(filepath.Join(projectDir, target))
	}
	if !isPathWithinDir(absPath, projectDir) {
		return "", "artifact ref_path is outside the project root"
	}
	data, err := os.ReadFile(absPath)
	if err != nil {
		return "", err.Error()
	}
	if len(data) > maxArtifactContentBytes {
		return "", fmt.Sprintf("artifact content exceeds %d bytes", maxArtifactContentBytes)
	}
	if bytes.IndexByte(data, 0) >= 0 || !utf8.Valid(data) {
		return "", "artifact content is not valid UTF-8 text"
	}
	return string(data), ""
}
