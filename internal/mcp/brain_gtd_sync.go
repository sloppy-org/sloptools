package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
)

type gtdSyncAction struct {
	Path     string `json:"path"`
	Binding  string `json:"binding"`
	Provider string `json:"provider"`
	Action   string `json:"action"`
	DryRun   bool   `json:"dry_run,omitempty"`
}

type gtdSyncDrift struct {
	Path    string `json:"path"`
	Binding string `json:"binding"`
	Local   string `json:"local"`
	Remote  string `json:"remote"`
}

type gtdSyncError struct {
	Path    string `json:"path"`
	Binding string `json:"binding"`
	Error   string `json:"error"`
}

type gtdSyncState struct {
	Status   string
	ClosedAt string
}

func (s *Server) brainGTDSync(args map[string]interface{}) (map[string]interface{}, error) {
	notes, err := s.loadSyncNotes(args)
	if err != nil {
		return nil, err
	}
	sources, err := loadGTDSources(strArg(args, "sources_config"))
	if err != nil {
		return nil, err
	}
	periodic := boolArg(args, "periodic")
	dryRun := boolArg(args, "dry_run")
	var actions []gtdSyncAction
	var drifts []gtdSyncDrift
	var syncErrs []gtdSyncError
	reconciled, skipped := 0, 0
	for _, note := range notes {
		for _, binding := range note.Entry.Commitment.SourceBindings {
			if !sources.writeable(note, binding) {
				skipped++
				continue
			}
			if periodic {
				changed, action, drift, err := s.periodicSyncBinding(note, binding, dryRun)
				if action.Binding != "" {
					actions = append(actions, action)
				}
				if drift.Binding != "" {
					drifts = append(drifts, drift)
				}
				if err != nil {
					syncErrs = append(syncErrs, syncError(note, binding, err))
					continue
				}
				if changed {
					reconciled++
				} else {
					skipped++
				}
				continue
			}
			if !commitmentClosed(note.Entry.Commitment) {
				skipped++
				continue
			}
			action, err := s.pushClosedBinding(note, binding, args, dryRun)
			if action.Binding != "" {
				actions = append(actions, action)
			}
			if err != nil {
				syncErrs = append(syncErrs, syncError(note, binding, err))
				continue
			}
			reconciled++
		}
	}
	return map[string]interface{}{
		"sphere":     strArg(args, "sphere"),
		"periodic":   periodic,
		"dry_run":    dryRun,
		"reconciled": reconciled,
		"drifted":    drifts,
		"skipped":    skipped,
		"errors":     syncErrs,
		"actions":    actions,
	}, nil
}

func (s *Server) brainGTDSetStatus(args map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(strArg(args, "path")) == "" && strings.TrimSpace(strArg(args, "commitment_id")) == "" {
		return nil, errors.New("path or commitment_id is required")
	}
	notes, err := s.loadSyncNotes(args)
	if err != nil {
		return nil, err
	}
	status := strings.TrimSpace(strArg(args, "status"))
	if status == "" {
		return nil, errors.New("status is required")
	}
	note := notes[0]
	commitment := note.Entry.Commitment
	commitment.LocalOverlay.Status = status
	if closedStatus(status) {
		if commitment.LocalOverlay.ClosedAt == "" {
			commitment.LocalOverlay.ClosedAt = strings.TrimSpace(strArg(args, "closed_at"))
		}
		if commitment.LocalOverlay.ClosedAt == "" {
			commitment.LocalOverlay.ClosedAt = time.Now().UTC().Format(time.RFC3339)
		}
		closedVia := strings.TrimSpace(strArg(args, "closed_via"))
		if closedVia == "" {
			closedVia = "brain.gtd.set_status"
		}
		commitment.LocalOverlay.ClosedVia = closedVia
	}
	note.Entry.Commitment = commitment
	if err := writeDedupNotes(note); err != nil {
		return nil, err
	}
	syncArgs := copyArgs(args)
	syncArgs["path"] = note.Entry.Path
	syncArgs["commitment_id"] = note.Entry.Path
	syncResult, syncErr := s.brainGTDSync(syncArgs)
	if syncErr != nil {
		return nil, syncErr
	}
	return map[string]interface{}{
		"sphere":        strArg(args, "sphere"),
		"path":          note.Entry.Path,
		"status":        status,
		"local_overlay": commitment.LocalOverlay,
		"sync":          syncResult,
	}, nil
}

func (s *Server) loadSyncNotes(args map[string]interface{}) ([]dedupNote, error) {
	notes, _, err := s.loadDedupNotes(args)
	if err != nil {
		return nil, err
	}
	path := strings.TrimSpace(strArg(args, "path"))
	if path == "" {
		path = strings.TrimSpace(strArg(args, "commitment_id"))
	}
	if path == "" {
		return notes, nil
	}
	for _, note := range notes {
		if note.Entry.Path == path {
			return []dedupNote{note}, nil
		}
	}
	return nil, fmt.Errorf("commitment %q not found", path)
}

func (s *Server) pushClosedBinding(note dedupNote, binding braingtd.SourceBinding, args map[string]interface{}, dryRun bool) (gtdSyncAction, error) {
	action := syncAction(note, binding, "close_upstream", dryRun)
	if dryRun {
		return action, nil
	}
	switch gtdSyncProvider(binding.Provider) {
	case "manual":
		return syncAction(note, binding, "manual_noop", false), nil
	case "meetings":
		return action, closeMeetingBinding(note, binding)
	case "mail":
		return action, s.closeMailBinding(note, binding, args)
	case "todoist":
		return action, s.closeTodoistBinding(note, binding, args)
	case "github":
		return action, closeGitHubBinding(binding)
	case "gitlab":
		return action, closeGitLabBinding(binding)
	default:
		return action, fmt.Errorf("unsupported writeable binding provider %q", binding.Provider)
	}
}

func (s *Server) periodicSyncBinding(note dedupNote, binding braingtd.SourceBinding, dryRun bool) (bool, gtdSyncAction, gtdSyncDrift, error) {
	if gtdSyncProvider(binding.Provider) == "manual" {
		return false, gtdSyncAction{}, gtdSyncDrift{}, nil
	}
	state, err := s.readBindingState(note, binding)
	if err != nil {
		return false, gtdSyncAction{}, gtdSyncDrift{}, err
	}
	localClosed := commitmentClosed(note.Entry.Commitment)
	switch {
	case state.Status == "closed" && !localClosed:
		action := syncAction(note, binding, "close_local_overlay", dryRun)
		if !dryRun {
			commitment := note.Entry.Commitment
			commitment.LocalOverlay.Status = "closed"
			commitment.LocalOverlay.ClosedVia = "brain.gtd.sync"
			if commitment.LocalOverlay.ClosedAt == "" {
				commitment.LocalOverlay.ClosedAt = syncClosedAt(state)
			}
			note.Entry.Commitment = commitment
			if err := writeDedupNotes(note); err != nil {
				return false, action, gtdSyncDrift{}, err
			}
		}
		return true, action, gtdSyncDrift{}, nil
	case state.Status == "open" && localClosed:
		return false, gtdSyncAction{}, gtdSyncDrift{Path: note.Entry.Path, Binding: binding.StableID(), Local: "closed", Remote: "open"}, nil
	default:
		return false, gtdSyncAction{}, gtdSyncDrift{}, nil
	}
}

func (s *Server) readBindingState(note dedupNote, binding braingtd.SourceBinding) (gtdSyncState, error) {
	switch gtdSyncProvider(binding.Provider) {
	case "manual":
		return gtdSyncState{Status: "open"}, nil
	case "meetings":
		return readMeetingBindingState(note, binding)
	case "mail":
		return s.readMailBindingState(note, binding)
	case "todoist":
		return s.readTodoistBindingState(note, binding)
	case "github":
		return readGitHubBindingState(binding)
	case "gitlab":
		return readGitLabBindingState(binding)
	default:
		return gtdSyncState{}, fmt.Errorf("periodic read is not implemented for provider %q", binding.Provider)
	}
}

func closeMeetingBinding(note dedupNote, binding braingtd.SourceBinding) error {
	path, err := resolveBindingFile(note, binding)
	if err != nil {
		return err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	lines := strings.SplitAfter(string(data), "\n")
	index, err := matchingCheckboxLine(lines, binding)
	if err != nil {
		return err
	}
	if !strings.Contains(lines[index], "[ ]") {
		return nil
	}
	lines[index] = strings.Replace(lines[index], "[ ]", "[x]", 1)
	return os.WriteFile(path, []byte(strings.Join(lines, "")), 0o644)
}

func readMeetingBindingState(note dedupNote, binding braingtd.SourceBinding) (gtdSyncState, error) {
	path, err := resolveBindingFile(note, binding)
	if err != nil {
		return gtdSyncState{}, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return gtdSyncState{}, err
	}
	lines := strings.SplitAfter(string(data), "\n")
	index, err := matchingCheckboxLine(lines, binding)
	if err != nil {
		return gtdSyncState{}, err
	}
	line := lines[index]
	if strings.Contains(line, "[x]") || strings.Contains(line, "[X]") {
		return gtdSyncState{Status: "closed"}, nil
	}
	if strings.Contains(line, "[ ]") {
		return gtdSyncState{Status: "open"}, nil
	}
	return gtdSyncState{}, fmt.Errorf("matching meeting line has no checkbox")
}

func resolveBindingFile(note dedupNote, binding braingtd.SourceBinding) (string, error) {
	raw := strings.TrimSpace(binding.Location.Path)
	if raw == "" {
		return "", fmt.Errorf("binding location.path is required")
	}
	if filepath.IsAbs(raw) {
		if !isPathWithin(note.Resolved.VaultRoot, raw) {
			return "", fmt.Errorf("binding path %q is outside vault", raw)
		}
		return filepath.Clean(raw), nil
	}
	candidates := []string{
		filepath.Join(note.Resolved.VaultRoot, filepath.FromSlash(raw)),
		filepath.Join(note.Resolved.BrainRoot, filepath.FromSlash(raw)),
	}
	for _, candidate := range candidates {
		if _, err := os.Stat(candidate); err == nil {
			return filepath.Clean(candidate), nil
		}
	}
	return filepath.Clean(candidates[0]), nil
}

func matchingCheckboxLine(lines []string, binding braingtd.SourceBinding) (int, error) {
	needles := compactSyncStrings([]string{binding.Location.Anchor, binding.Ref})
	for i, line := range lines {
		if !strings.Contains(line, "[ ]") && !strings.Contains(line, "[x]") && !strings.Contains(line, "[X]") {
			continue
		}
		if len(needles) == 0 {
			return i, nil
		}
		for _, needle := range needles {
			if strings.Contains(line, needle) {
				return i, nil
			}
		}
	}
	return -1, fmt.Errorf("matching meeting checkbox not found for %q", binding.StableID())
}

func (s *Server) closeMailBinding(note dedupNote, binding braingtd.SourceBinding, args map[string]interface{}) error {
	account, provider, err := s.mailProviderForSync(note, args)
	if err != nil {
		return err
	}
	defer provider.Close()
	messageID := strings.TrimSpace(binding.Ref)
	if messageID == "" {
		return errors.New("mail binding ref is required")
	}
	if _, err := provider.MarkRead(context.Background(), []string{messageID}); err != nil {
		return err
	}
	action := strings.TrimSpace(strings.ToLower(strArg(args, "mail_action")))
	if action == "" {
		action = "archive"
	}
	_, err = applyMailActionGeneric(context.Background(), account, provider, action, []string{messageID}, strArg(args, "mail_folder"), strArg(args, "mail_label"), nil, time.Time{})
	return err
}

func (s *Server) readMailBindingState(note dedupNote, binding braingtd.SourceBinding) (gtdSyncState, error) {
	_, provider, err := s.mailProviderForSync(note, nil)
	if err != nil {
		return gtdSyncState{}, err
	}
	defer provider.Close()
	message, err := provider.GetMessage(context.Background(), strings.TrimSpace(binding.Ref), "metadata")
	if err != nil {
		return gtdSyncState{}, err
	}
	if message == nil {
		return gtdSyncState{}, fmt.Errorf("mail message %q not found", binding.Ref)
	}
	if message.IsRead && !mailLabelsContain(message.Labels, "inbox") {
		return gtdSyncState{Status: "closed"}, nil
	}
	return gtdSyncState{Status: "open"}, nil
}

func (s *Server) closeTodoistBinding(note dedupNote, binding braingtd.SourceBinding, args map[string]interface{}) error {
	_, provider, err := s.tasksProviderForSync(note, store.ExternalProviderTodoist)
	if err != nil {
		return err
	}
	defer provider.Close()
	completer, ok := groupware.Supports[tasks.Completer](provider)
	if !ok {
		return fmt.Errorf("provider %s does not support task completion", provider.ProviderName())
	}
	listID, taskID := splitTaskBindingRef(binding.Ref)
	if listID == "" {
		listID = strings.TrimSpace(strArg(args, "todoist_list_id"))
	}
	if taskID == "" {
		return errors.New("todoist binding ref is required")
	}
	return completer.CompleteTask(context.Background(), listID, taskID)
}

func (s *Server) readTodoistBindingState(note dedupNote, binding braingtd.SourceBinding) (gtdSyncState, error) {
	_, provider, err := s.tasksProviderForSync(note, store.ExternalProviderTodoist)
	if err != nil {
		return gtdSyncState{}, err
	}
	defer provider.Close()
	listID, taskID := splitTaskBindingRef(binding.Ref)
	item, err := provider.GetTask(context.Background(), listID, taskID)
	if err != nil {
		return gtdSyncState{}, err
	}
	if item.Completed {
		return gtdSyncState{Status: "closed"}, nil
	}
	return gtdSyncState{Status: "open"}, nil
}

func (s *Server) mailProviderForSync(note dedupNote, args map[string]interface{}) (store.ExternalAccount, email.EmailProvider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	if args != nil {
		accountID, _, err := optionalInt64Arg(args, "account_id")
		if err != nil {
			return store.ExternalAccount{}, nil, err
		}
		if accountID != nil {
			account, err := st.GetExternalAccount(*accountID)
			if err != nil {
				return store.ExternalAccount{}, nil, err
			}
			if !account.Enabled || !emailCapableProvider(account.Provider) {
				return store.ExternalAccount{}, nil, fmt.Errorf("account %d does not support email sync", account.ID)
			}
			provider, err := s.emailProviderForAccount(context.Background(), account)
			if err != nil {
				return store.ExternalAccount{}, nil, err
			}
			return account, provider, nil
		}
	}
	account, err := firstProviderAccount(st, string(note.Resolved.Sphere), "", emailCapableProvider)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.emailProviderForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (s *Server) tasksProviderForSync(note dedupNote, providerName string) (store.ExternalAccount, tasks.Provider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := firstProviderAccount(st, string(note.Resolved.Sphere), providerName, isTasksCapableProvider)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.tasksProviderForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}
