package brain

import (
	"fmt"
	"path/filepath"
	"strings"
)

// nullDestination is the sentinel for delete-with-link-audit moves.
const nullDestination = "/dev/null"

// archivalLogRel is the per-vault archival audit log path.
var archivalLogRel = filepath.Join("brain", "generated", "archival-log.md")

// PlanMove computes the link-aware move plan for the named sphere. The plan
// is canonicalized so a later ApplyMove call can refuse to run if the world
// changed since the dry-run.
func PlanMove(cfg *Config, sphere Sphere, from, to string) (*MovePlan, error) {
	if cfg == nil {
		return nil, fmt.Errorf("brain move: config is nil")
	}
	srcVault, ok := cfg.Vault(sphere)
	if !ok {
		return nil, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	fromRel, fromAbs, err := normalizeMoveSide(srcVault, from, "from")
	if err != nil {
		return nil, err
	}
	deleting := strings.TrimSpace(to) == nullDestination
	var toRel, toAbs string
	if !deleting {
		toRel, toAbs, err = normalizeMoveSide(srcVault, to, "to")
		if err != nil {
			return nil, err
		}
		if fromRel == toRel {
			return nil, fmt.Errorf("brain move: from and to resolve to the same path %q", fromRel)
		}
		if isWithin(fromAbs, toAbs) {
			return nil, fmt.Errorf("brain move: destination %q is inside source %q", toRel, fromRel)
		}
	}
	files, err := collectFileMoves(srcVault, fromRel, fromAbs, toRel, deleting)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("brain move: source %q is empty or missing", fromRel)
	}

	plan := &MovePlan{
		Sphere: srcVault.Sphere,
		From:   filepath.ToSlash(fromRel),
		Files:  files,
	}
	if deleting {
		plan.To = nullDestination
	} else {
		plan.To = filepath.ToSlash(toRel)
	}

	movedSet := makeMovedSet(files)
	edits, err := collectInboundEdits(cfg, srcVault, fromRel, toRel, deleting, movedSet)
	if err != nil {
		return nil, err
	}
	plan.Edits = edits

	if !deleting {
		inner, err := collectInnerEdits(srcVault, files, fromRel, toRel)
		if err != nil {
			return nil, err
		}
		plan.Inner = inner
	}

	if deleting {
		if warnings := inboundWarnings(plan.Edits); len(warnings) > 0 {
			plan.Notes = append(plan.Notes, warnings...)
		}
	}

	plan.Digest = canonicalDigest(plan)
	return plan, nil
}

// PlanMerge produces a plan that deletes the loser file and rewrites every
// inbound reference (wikilink or relative Markdown link) so it points at
// survivor instead of being stripped. Used by `brain consolidate apply
// --merge`. The survivor file is not moved; the caller writes merged
// content into it before calling ApplyMove.
//
// The plan reports To == "/dev/null" so applyFileMoves performs a deletion;
// MergeTarget records survivor for digest stability and so ApplyMove
// re-derives via PlanMerge rather than PlanMove.
func PlanMerge(cfg *Config, sphere Sphere, loser, survivor string) (*MovePlan, error) {
	if cfg == nil {
		return nil, fmt.Errorf("brain merge: config is nil")
	}
	srcVault, ok := cfg.Vault(sphere)
	if !ok {
		return nil, &PathError{Kind: ErrorUnknownVault, Sphere: normalizeSphere(sphere)}
	}
	loserRel, loserAbs, err := normalizeMoveSide(srcVault, loser, "from")
	if err != nil {
		return nil, err
	}
	survivorRel, _, err := normalizeMoveSide(srcVault, survivor, "to")
	if err != nil {
		return nil, err
	}
	if loserRel == survivorRel {
		return nil, fmt.Errorf("brain merge: loser and survivor resolve to the same path %q", loserRel)
	}
	files, err := collectFileMoves(srcVault, loserRel, loserAbs, "", true)
	if err != nil {
		return nil, err
	}
	if len(files) == 0 {
		return nil, fmt.Errorf("brain merge: loser %q is empty or missing", loserRel)
	}
	movedSet := makeMovedSet(files)
	// deleting=false here triggers the redirect rewrite path:
	// every [[loserRel]] is rewritten to [[survivorRel]] instead of being
	// stripped to plain text.
	edits, err := collectInboundEdits(cfg, srcVault, loserRel, survivorRel, false, movedSet)
	if err != nil {
		return nil, err
	}
	plan := &MovePlan{
		Sphere:      srcVault.Sphere,
		From:        filepath.ToSlash(loserRel),
		To:          nullDestination,
		MergeTarget: filepath.ToSlash(survivorRel),
		Files:       files,
		Edits:       edits,
		Notes:       []string{"merge: redirect inbound links to " + filepath.ToSlash(survivorRel)},
	}
	plan.Digest = canonicalDigest(plan)
	return plan, nil
}

// ApplyMove executes the plan after re-deriving it from disk and comparing
// the digest. The caller must pass the digest from the dry-run as confirm.
func ApplyMove(cfg *Config, plan *MovePlan, confirm string) error {
	if plan == nil {
		return fmt.Errorf("brain move: plan is nil")
	}
	if confirm != plan.Digest {
		return fmt.Errorf("brain move: confirm digest %q does not match plan digest %q", confirm, plan.Digest)
	}
	var fresh *MovePlan
	var err error
	if plan.MergeTarget != "" {
		fresh, err = PlanMerge(cfg, plan.Sphere, plan.From, plan.MergeTarget)
	} else {
		fresh, err = PlanMove(cfg, plan.Sphere, plan.From, plan.To)
	}
	if err != nil {
		return fmt.Errorf("brain move: re-derive plan: %w", err)
	}
	if fresh.Digest != plan.Digest {
		return fmt.Errorf("brain move: plan digest changed since dry-run (have %q, fresh %q)", plan.Digest, fresh.Digest)
	}
	if plan.To == nullDestination && plan.MergeTarget == "" && strings.Join(fresh.Notes, "\n") != strings.Join(plan.Notes, "\n") {
		return fmt.Errorf("brain move: inbound link warnings changed since dry-run")
	}
	vault, ok := cfg.Vault(plan.Sphere)
	if !ok {
		return &PathError{Kind: ErrorUnknownVault, Sphere: plan.Sphere}
	}
	if err := applyFileMoves(vault, fresh); err != nil {
		return err
	}
	if err := applyEdits(cfg, fresh.Edits); err != nil {
		return err
	}
	innerWithDest := translateInnerToDestination(fresh)
	if err := applyInnerEdits(vault, innerWithDest); err != nil {
		return err
	}
	return writeArchivalLog(vault, fresh)
}

// translateInnerToDestination remaps Inner LinkEdit paths from their source
// (pre-move) location to the destination, so applyInnerEdits can find the
// files after applyFileMoves runs.
func translateInnerToDestination(plan *MovePlan) []LinkEdit {
	out := make([]LinkEdit, 0, len(plan.Inner))
	for _, edit := range plan.Inner {
		newEdit := edit
		newEdit.Path = destinationFor(edit.Path, plan.From, plan.To, false)
		out = append(out, newEdit)
	}
	return out
}
