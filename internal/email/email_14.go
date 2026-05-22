package email

import (
	"context"
	"fmt"
	"strings"
)

// RecoverFromDumpster moves messages currently held in the Recoverable Items
// dumpster (recoverableitemsdeletions) into targetFolder. The target is
// resolved like a regular folder path and created under Archive when missing:
// a bare "Recovered-X" becomes "Archive/Recovered-X". Returns the action
// resolutions (resolved IDs after the move).
func (p *ExchangeEWSMailProvider) RecoverFromDumpster(ctx context.Context, messageIDs []string, targetFolder string) ([]ActionResolution, error) {
	ids := compactMessageIDs(messageIDs)
	if len(ids) == 0 {
		return nil, nil
	}
	folderID, err := p.ensureRecoverTargetFolder(ctx, targetFolder)
	if err != nil {
		return nil, err
	}
	resolved, err := p.client.MoveItems(ctx, ids, folderID)
	if err != nil {
		return nil, err
	}
	return actionResolutions(ids, resolved), nil
}

// ensureRecoverTargetFolder resolves the desired target folder, creating it
// under Archive when a bare leaf name was supplied or when "Archive/Leaf" does
// not yet exist. Two-level paths are supported (parent/leaf); deeper paths are
// resolved via the standard folder lookup.
func (p *ExchangeEWSMailProvider) ensureRecoverTargetFolder(ctx context.Context, target string) (string, error) {
	clean := strings.TrimSpace(target)
	if clean == "" {
		return "", fmt.Errorf("recover target folder is required")
	}
	parts := strings.Split(clean, "/")
	for i := range parts {
		parts[i] = strings.TrimSpace(parts[i])
	}
	if len(parts) == 1 {
		parts = []string{p.cfg.ArchiveFolder, parts[0]}
	}
	if len(parts) != 2 {
		return p.resolveFolderRef(ctx, clean)
	}
	parentName, leaf := parts[0], parts[1]
	if leaf == "" {
		return "", fmt.Errorf("recover target folder leaf is empty")
	}
	var parentID string
	if strings.EqualFold(parentName, "Archive") || strings.EqualFold(parentName, p.cfg.ArchiveFolder) {
		id, err := p.resolveArchiveFolderID(ctx)
		if err != nil {
			return "", err
		}
		parentID = id
	} else {
		id, err := p.resolveFolderRef(ctx, parentName)
		if err != nil {
			return "", err
		}
		parentID = id
	}
	if strings.TrimSpace(parentID) == "" {
		return "", fmt.Errorf("recover parent folder %q not found", parentName)
	}
	existing, err := p.client.FindMailSubfolder(ctx, parentID, leaf)
	if err != nil {
		return "", err
	}
	if existing != nil && strings.TrimSpace(existing.ID) != "" {
		return existing.ID, nil
	}
	newID, _, err := p.client.CreateMailFolder(ctx, parentID, leaf)
	if err != nil {
		return "", fmt.Errorf("create recover target %q under %q: %w", leaf, parentName, err)
	}
	return newID, nil
}
