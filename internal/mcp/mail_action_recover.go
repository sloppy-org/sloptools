package mcp

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/store"
)

// mailActionRecover implements mail_action=recover: moves items currently held
// in the server-side Recoverable Items dumpster into a target folder, creating
// the target under Archive when missing. Search filters (after/before/from/
// subject/query) scope the dumpster listing; dry_run=true returns the
// candidate list without performing the move.
func (s *Server) mailActionRecover(ctx context.Context, st *store.Store, account store.ExternalAccount, provider email.EmailProvider, args map[string]interface{}) (map[string]interface{}, error) {
	recoverer, ok := provider.(email.RecoverProvider)
	if !ok {
		return nil, fmt.Errorf("recover not supported for provider %s", account.Provider)
	}
	target := strings.TrimSpace(strArg(args, "folder"))
	if target == "" {
		target = "Archive/Recovered-" + time.Now().Format("2006-01-02")
	}
	listArgs := make(map[string]interface{}, len(args)+2)
	for k, v := range args {
		listArgs[k] = v
	}
	listArgs["folder"] = "recoverableitemsdeletions"
	if _, present := listArgs["limit"]; !present {
		listArgs["limit"] = 1000
	}
	ids, err := resolveDumpsterMessageIDs(ctx, provider, listArgs)
	if err != nil {
		return nil, err
	}
	dryRun, _ := args["dry_run"].(bool)
	result := map[string]interface{}{
		"account":     account,
		"action":      "recover",
		"target":      target,
		"count":       len(ids),
		"message_ids": append([]string(nil), ids...),
	}
	if dryRun {
		result["dry_run"] = true
		return result, nil
	}
	if len(ids) == 0 {
		result["recovered"] = 0
		return result, nil
	}
	logs := make([]store.MailActionLog, 0, len(ids))
	for _, id := range ids {
		entry, logErr := st.CreateMailActionLog(store.MailActionLogInput{
			AccountID: account.ID, Provider: account.Provider, MessageID: id,
			Action: "recover", FolderTo: target,
			Request: map[string]any{"action": "recover", "target": target},
			Status:  store.MailActionLogPending,
		})
		if logErr != nil {
			return nil, logErr
		}
		logs = append(logs, entry)
	}
	resolutions, err := recoverer.RecoverFromDumpster(ctx, ids, target)
	if err != nil {
		for _, entry := range logs {
			_ = st.UpdateMailActionLogResult(entry.ID, store.MailActionLogFailed, "", err.Error())
		}
		return nil, err
	}
	resolved := make(map[string]string, len(resolutions))
	for _, r := range resolutions {
		resolved[strings.TrimSpace(r.OriginalMessageID)] = strings.TrimSpace(r.ResolvedMessageID)
	}
	for _, entry := range logs {
		_ = st.UpdateMailActionLogResult(entry.ID, store.MailActionLogApplied, resolved[strings.TrimSpace(entry.MessageID)], "")
	}
	result["recovered"] = len(resolutions)
	result["resolutions"] = resolutions
	return result, nil
}

// resolveDumpsterMessageIDs lists message IDs in the recoverable-items
// dumpster according to the given args. Unlike resolveMailActionMessageIDs it
// does not require a query — recovery is commonly bounded by date filters
// alone.
func resolveDumpsterMessageIDs(ctx context.Context, provider email.EmailProvider, args map[string]interface{}) ([]string, error) {
	if ids := mailMessageIDsArg(args); len(ids) > 0 {
		return ids, nil
	}
	opts, _, err := mailSearchOptionsFromArgs(args)
	if err != nil {
		return nil, err
	}
	ids, _, err := listMailMessageIDs(ctx, provider, opts, "")
	if err != nil {
		return nil, err
	}
	return compactStringList(ids), nil
}
