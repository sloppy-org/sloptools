package calendar

import (
	"fmt"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func GoogleCalendarAccounts(st *store.Store) ([]store.ExternalAccount, error) {
	if st == nil {
		return nil, nil
	}
	accounts, err := st.ListExternalAccountsByProvider(store.ExternalProviderGoogleCalendar)
	if err != nil {
		return nil, err
	}
	accounts = enabledAccounts(accounts)
	if len(accounts) > 0 {
		return accounts, nil
	}
	gmailAccounts, err := st.ListExternalAccountsByProvider(store.ExternalProviderGmail)
	if err != nil {
		return nil, err
	}
	gmailAccounts = enabledAccounts(gmailAccounts)
	out := make([]store.ExternalAccount, 0, len(gmailAccounts))
	for _, account := range gmailAccounts {
		account.Provider = store.ExternalProviderGoogleCalendar
		out = append(out, account)
	}
	return out, nil
}

func ResolveCalendarSphere(st *store.Store, provider, calendarID, calendarName, fallback string, accounts []store.ExternalAccount) string {
	if st != nil {
		for _, ref := range []string{calendarID, calendarName} {
			if strings.TrimSpace(ref) == "" {
				continue
			}
			mapping, err := st.GetContainerMapping(provider, "calendar", ref)
			if err != nil {
				continue
			}
			if mapping.Sphere != nil && strings.TrimSpace(*mapping.Sphere) != "" {
				return strings.TrimSpace(*mapping.Sphere)
			}
			if mapping.WorkspaceID != nil {
				workspace, workspaceErr := st.GetWorkspace(*mapping.WorkspaceID)
				if workspaceErr == nil && strings.TrimSpace(workspace.Sphere) != "" {
					return workspace.Sphere
				}
			}
		}
	}
	for _, account := range accounts {
		if strings.EqualFold(strings.TrimSpace(account.AccountName), strings.TrimSpace(calendarName)) ||
			strings.EqualFold(strings.TrimSpace(account.AccountName), strings.TrimSpace(calendarID)) {
			return account.Sphere
		}
	}
	if len(accounts) == 1 && strings.TrimSpace(accounts[0].Sphere) != "" {
		return accounts[0].Sphere
	}
	if strings.TrimSpace(fallback) != "" {
		return fallback
	}
	return store.SpherePrivate
}

func enabledAccounts(accounts []store.ExternalAccount) []store.ExternalAccount {
	out := make([]store.ExternalAccount, 0, len(accounts))
	for _, account := range accounts {
		if account.Enabled {
			out = append(out, account)
		}
	}
	return out
}

func SelectCalendar(calendars []providerdata.Calendar, st *store.Store, accounts []store.ExternalAccount, calendarRef, preferredSphere string) (providerdata.Calendar, error) {
	if len(calendars) == 0 {
		return providerdata.Calendar{}, fmt.Errorf("no calendars are available")
	}
	ref := strings.TrimSpace(calendarRef)
	if ref != "" {
		for _, cal := range calendars {
			if strings.EqualFold(strings.TrimSpace(cal.ID), ref) || strings.EqualFold(strings.TrimSpace(cal.Name), ref) {
				return cal, nil
			}
		}
		return providerdata.Calendar{}, fmt.Errorf("calendar %q not found", ref)
	}
	sphere := strings.TrimSpace(preferredSphere)
	if sphere != "" {
		matches := make([]providerdata.Calendar, 0, len(calendars))
		for _, cal := range calendars {
			if strings.EqualFold(ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, "", accounts), sphere) {
				matches = append(matches, cal)
			}
		}
		if len(matches) == 1 {
			return matches[0], nil
		}
		for _, cal := range matches {
			if cal.Primary {
				return cal, nil
			}
		}
		if len(matches) > 0 {
			return matches[0], nil
		}
	}
	if len(calendars) == 1 {
		return calendars[0], nil
	}
	for _, cal := range calendars {
		if cal.Primary {
			return cal, nil
		}
	}
	return providerdata.Calendar{}, fmt.Errorf("multiple calendars are available; specify calendar_id")
}
