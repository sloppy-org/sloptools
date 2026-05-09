package mcp

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

// mailBindingMatchesMessage reports whether a SourceBinding's ref points at
// the given mail message. We accept three storage conventions seen in the
// vault:
//   - bare message ID (most current commitments)
//   - mail:<sphere>:<account>:<id>
//   - mail:<id>
//
// All comparisons are case-insensitive on the message-ID component.
func mailBindingMatchesMessage(binding braingtd.SourceBinding, sphere string, accountID int64, messageID string) bool {
	if gtdSyncProvider(binding.Provider) != "mail" {
		return false
	}
	target := strings.TrimSpace(messageID)
	if target == "" {
		return false
	}
	ref := strings.TrimSpace(binding.Ref)
	if ref == "" {
		return false
	}
	if strings.EqualFold(ref, target) {
		return true
	}
	parts := strings.Split(ref, ":")
	tail := strings.TrimSpace(parts[len(parts)-1])
	if strings.EqualFold(tail, target) {
		return true
	}
	prefix := fmt.Sprintf("mail:%s:%d:", strings.ToLower(strings.TrimSpace(sphere)), accountID)
	if strings.EqualFold(ref[:minInt(len(prefix), len(ref))], prefix) {
		remainder := ref[len(prefix):]
		return strings.EqualFold(strings.TrimSpace(remainder), target)
	}
	return false
}

const defaultHandoffMaxConsumes = 1

type handoffRegistry struct {
	mu       sync.Mutex
	handoffs map[string]*storedHandoff
}

type storedHandoff struct {
	envelope      handoffEnvelope
	expiresAt     time.Time
	maxConsumes   int
	consumedCount int
	revoked       bool
}

func newHandoffRegistry() *handoffRegistry {
	return &handoffRegistry{handoffs: map[string]*storedHandoff{}}
}

func (s *Server) handoffCreate(args map[string]interface{}) (map[string]interface{}, error) {
	kind := strings.TrimSpace(strArg(args, "kind"))
	if kind == "" {
		return nil, errors.New("kind is required")
	}
	selector, _ := args["selector"].(map[string]interface{})
	if selector == nil {
		return nil, errors.New("selector is required")
	}
	policy, _ := args["policy"].(map[string]interface{})
	switch kind {
	case handoffKindMail:
		return s.mailHandoffCreate(selector, policy)
	default:
		return nil, fmt.Errorf("unsupported handoff kind: %s", kind)
	}
}

func (s *Server) handoffPeek(args map[string]interface{}) (map[string]interface{}, error) {
	record, err := s.lookupHandoff(args)
	if err != nil {
		return nil, err
	}
	return handoffSummary(record), nil
}

func (s *Server) handoffConsume(args map[string]interface{}) (map[string]interface{}, error) {
	record, err := s.lookupHandoff(args)
	if err != nil {
		return nil, err
	}
	return s.handoffs.consume(record.envelope.HandoffID)
}

func (s *Server) handoffRevoke(args map[string]interface{}) (map[string]interface{}, error) {
	record, err := s.lookupHandoff(args)
	if err != nil {
		return nil, err
	}
	return s.handoffs.revoke(record.envelope.HandoffID)
}

func (s *Server) handoffStatus(args map[string]interface{}) (map[string]interface{}, error) {
	record, err := s.lookupHandoff(args)
	if err != nil {
		return nil, err
	}
	return handoffStatus(record), nil
}

func (s *Server) lookupHandoff(args map[string]interface{}) (*storedHandoff, error) {
	handoffID := strings.TrimSpace(strArg(args, "handoff_id"))
	if handoffID == "" {
		return nil, errors.New("handoff_id is required")
	}
	return s.handoffs.lookup(handoffID)
}

func (s *Server) mailHandoffCreate(selector, policy map[string]interface{}) (map[string]interface{}, error) {
	account, provider, messageIDs, err := s.mailHandoffSelection(selector)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	messages, err := provider.GetMessages(context.Background(), messageIDs, "full")
	if err != nil {
		return nil, err
	}
	orderedMessages, err := orderedMailMessages(messageIDs, messages)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	handoffID, err := newHandoffID(handoffKindMail)
	if err != nil {
		return nil, err
	}
	policyState, err := parseHandoffPolicy(policy, now)
	if err != nil {
		return nil, err
	}
	envelope := handoffEnvelope{SpecVersion: "handoff.v1", HandoffID: handoffID, Kind: handoffKindMail, CreatedAt: now.Format(time.RFC3339), Meta: mailHandoffMeta(account, orderedMessages), Payload: map[string]interface{}{"messages": mailHandoffMessages(orderedMessages)}}
	record := &storedHandoff{envelope: envelope, expiresAt: policyState.expiresAt, maxConsumes: policyState.maxConsumes}
	s.handoffs.store(record)
	return handoffSummary(record), nil
}

// walkMailBindings walks every commitment markdown across both vaults and,
// for each binding that points at one of the mutated messages, recomputes
// the GTD status from the post-mutation provider state and rewrites the
// commitment. Returns the affected refs (mail messages plus brain
// commitments) and a count of commitments updated.
func (s *Server) walkMailBindings(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, messageIDs []string, brainCfg *brain.Config) ([]affectedRef, error) {
	if provider == nil {
		return nil, errors.New("walkMailBindings: provider is nil")
	}
	cleanIDs := compactStringList(messageIDs)
	if len(cleanIDs) == 0 || brainCfg == nil {
		return nil, nil
	}
	notes, err := loadAllVaultDedupNotes(brainCfg)
	if err != nil {
		return nil, err
	}
	if len(notes) == 0 {
		return nil, nil
	}
	messageState, err := mailFetchPostMutationState(ctx, provider, cleanIDs)
	if err != nil {
		return nil, err
	}
	waitingFolder := mailAccountWaitingFolder(account)
	updates := map[string]dedupNote{}
	refs := make([]affectedRef, 0, len(messageIDs)*2)
	seen := map[string]struct{}{}
	for _, note := range notes {
		sphere := string(note.Resolved.Sphere)
		mutated := false
		for i := range note.Entry.Commitment.SourceBindings {
			binding := note.Entry.Commitment.SourceBindings[i]
			for _, messageID := range cleanIDs {
				if !mailBindingMatchesMessage(binding, sphere, account.ID, messageID) {
					continue
				}
				message := messageState[messageID]
				if message == nil {
					// Message was hard-deleted or trashed beyond reach. Treat
					// as closed so the bound commitment moves out of the
					// active queue.
					message = &providerdata.EmailMessage{IsRead: true}
				}
				if applyMailDerivedStateToCommitment(&note.Entry.Commitment, message, sphere, waitingFolder, brainCfg) {
					mutated = true
				}
				key := note.Resolved.Path
				if _, ok := seen[key]; !ok {
					refs = append(refs, brainCommitmentAffectedRef(sphere, note.Entry.Path))
					seen[key] = struct{}{}
				}
			}
		}
		if mutated {
			updates[note.Resolved.Path] = note
		}
	}
	if len(updates) == 0 {
		return refs, nil
	}
	pending := make([]dedupNote, 0, len(updates))
	for _, note := range updates {
		pending = append(pending, note)
	}
	sort.Slice(pending, func(i, j int) bool { return pending[i].Resolved.Path < pending[j].Resolved.Path })
	bySphere := map[brain.Sphere][]dedupNote{}
	for _, note := range pending {
		bySphere[note.Resolved.Sphere] = append(bySphere[note.Resolved.Sphere], note)
	}
	for sphere, notes := range bySphere {
		if err := writeCommitmentNotes(brainCfg, sphere, notes...); err != nil {
			return refs, err
		}
	}
	return refs, nil
}

// writeCommitmentNotes persists every commitment field (status, follow-up,
// labels, project) so mail-derived classification flows survive a write
// cycle. writeDedupNotes is dedup-scoped and only rewrites a subset.
func writeCommitmentNotes(cfg *brain.Config, sphere brain.Sphere, notes ...dedupNote) error {
	return brain.WithGitCommit(cfg, sphere, fmt.Sprintf("brain mail bindings: %d commitment(s)", len(notes)), func() error {
		for _, note := range notes {
			if err := writeCommitmentFrontMatter(note.Note, note.Entry.Commitment); err != nil {
				return err
			}
			if err := braingtd.ApplyCommitment(note.Note, note.Entry.Commitment); err != nil {
				return err
			}
			rendered, err := note.Note.Render()
			if err != nil {
				return err
			}
			if err := validateRenderedBrainGTD(rendered); err != nil {
				return err
			}
			if err := os.WriteFile(note.Resolved.Path, []byte(rendered), 0o644); err != nil {
				return err
			}
		}
		return nil
	})
}

// mailFetchPostMutationState pulls the freshest metadata for each message
// after the mutation completed. Missing messages (e.g. deleted) map to
// nil and are treated as fully closed by the caller.
func mailFetchPostMutationState(ctx context.Context, provider email.EmailProvider, messageIDs []string) (map[string]*providerdata.EmailMessage, error) {
	out := make(map[string]*providerdata.EmailMessage, len(messageIDs))
	for _, messageID := range messageIDs {
		message, err := provider.GetMessage(ctx, messageID, "metadata")
		if err != nil {
			out[messageID] = nil
			continue
		}
		out[messageID] = message
	}
	return out, nil
}

// applyMailDerivedStateToCommitment updates the commitment's status,
// follow-up, labels, and project from the post-mutation message state.
// Returns true when at least one field changed.
func applyMailDerivedStateToCommitment(commitment *braingtd.Commitment, message *providerdata.EmailMessage, sphere, waitingFolder string, brainCfg *brain.Config) bool {
	derived := mailMessageToGTDStatus(message, waitingFolder)
	if derived.Status == "" {
		return false
	}
	changed := false
	if commitment.LocalOverlay.Status != derived.Status {
		commitment.LocalOverlay.Status = derived.Status
		changed = true
	}
	if commitment.LocalOverlay.FollowUp != derived.FollowUp {
		commitment.LocalOverlay.FollowUp = derived.FollowUp
		changed = true
	}
	if closedStatus(derived.Status) {
		if commitment.LocalOverlay.ClosedAt == "" {
			commitment.LocalOverlay.ClosedAt = time.Now().UTC().Format(time.RFC3339)
			changed = true
		}
		if commitment.LocalOverlay.ClosedVia == "" {
			commitment.LocalOverlay.ClosedVia = "mail.mutation"
			changed = true
		}
	} else {
		if commitment.LocalOverlay.ClosedAt != "" {
			commitment.LocalOverlay.ClosedAt = ""
			changed = true
		}
		if commitment.LocalOverlay.ClosedVia != "" {
			commitment.LocalOverlay.ClosedVia = ""
			changed = true
		}
	}
	folder := mailMessageFolder(message)
	classification := mailFolderToLabel(folder, sphere, brainCfg)
	if applyMailFolderClassification(commitment, classification) {
		changed = true
	}
	return changed
}

// applyMailFolderClassification merges a folder-derived classification
// into the commitment, replacing any existing track/* labels and any
// previous mail-derived project link.
func applyMailFolderClassification(commitment *braingtd.Commitment, classification mailFolderClassification) bool {
	changed := false
	existing := commitment.Labels
	preserved := make([]string, 0, len(existing))
	for _, label := range existing {
		clean := strings.TrimSpace(label)
		if clean == "" {
			continue
		}
		if isTrackLikeLabel(clean) {
			continue
		}
		preserved = append(preserved, clean)
	}
	merged := append(preserved, classification.Labels...)
	if !equalStringSlice(commitment.Labels, merged) {
		commitment.Labels = merged
		changed = true
	}
	if commitment.Project != classification.Project {
		commitment.Project = classification.Project
		changed = true
	}
	return changed
}

func isTrackLikeLabel(label string) bool {
	lower := strings.ToLower(strings.TrimSpace(label))
	return strings.HasPrefix(lower, "track/") || strings.HasPrefix(lower, "track:")
}

func equalStringSlice(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// loadAllVaultDedupNotes walks every commitment markdown across all
// configured vaults. Used by walkMailBindings so a mail mutation closes the
// bound commitment regardless of which sphere it lives in.
func loadAllVaultDedupNotes(cfg *brain.Config) ([]dedupNote, error) {
	if cfg == nil {
		return nil, nil
	}
	var all []dedupNote
	for _, vault := range cfg.Vaults {
		notes, err := readDedupNotesFromVault(vault)
		if err != nil {
			return nil, err
		}
		all = append(all, notes...)
	}
	return all, nil
}

// readDedupNotesFromVault is the vault-scoped variant of readDedupNotes.
// The dedup version walks the same path but is scoped to a single vault
// chosen via args; this helper lets callers iterate every vault.
func readDedupNotesFromVault(vault brain.Vault) ([]dedupNote, error) {
	var notes []dedupNote
	root := vault.BrainRoot()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if walkErr != nil || d.IsDir() || filepath.Ext(path) != ".md" {
			return walkErr
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		kind, _, kindDiags := brainNoteKind(string(data))
		if len(kindDiags) != 0 || kind != "commitment" {
			return nil
		}
		commitment, note, diags := braingtd.ParseCommitmentMarkdown(string(data))
		if len(diags) != 0 {
			return nil
		}
		rel, err := filepath.Rel(vault.Root, path)
		if err != nil {
			return err
		}
		resolved := brain.ResolvedPath{Sphere: vault.Sphere, VaultRoot: vault.Root, BrainRoot: root, Path: path, Rel: filepath.ToSlash(rel)}
		entry := braingtd.CommitmentEntry{Path: resolved.Rel, Commitment: *commitment}
		notes = append(notes, dedupNote{Entry: entry, Note: note, Resolved: resolved})
		return nil
	})
	if err != nil {
		return nil, err
	}
	return notes, nil
}

// mailBindingAffectedRefs walks bound commitments for a successful mail
// mutation. Errors are logged via the diagnostic channel but never abort
// the mail mutation itself — the binding walk is best-effort.
func (s *Server) mailBindingAffectedRefs(ctx context.Context, account store.ExternalAccount, provider email.EmailProvider, messageIDs []string) []affectedRef {
	if len(messageIDs) == 0 {
		return nil
	}
	brainCfg, err := loadMailBrainConfig(s.brainConfigPath)
	if err != nil || brainCfg == nil {
		return nil
	}
	refs, err := s.walkMailBindings(ctx, account, provider, messageIDs, brainCfg)
	if err != nil {
		return nil
	}
	return refs
}
