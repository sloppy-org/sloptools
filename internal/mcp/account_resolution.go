package mcp

import (
	"fmt"
	"sort"
	"strings"

	"github.com/sloppy-org/sloptools/internal/store"
)

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
