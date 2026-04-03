package calendar

import (
	"path/filepath"
	"testing"

	"github.com/sloppy-org/sloptools/internal/store"
)

func TestGoogleCalendarAccountsFallsBackToGmail(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "slopshell.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderGmail, "Gmail", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(gmail): %v", err)
	}
	accounts, err := GoogleCalendarAccounts(st)
	if err != nil {
		t.Fatalf("GoogleCalendarAccounts() error: %v", err)
	}
	if len(accounts) != 1 {
		t.Fatalf("account count = %d, want 1", len(accounts))
	}
	if accounts[0].Provider != store.ExternalProviderGoogleCalendar {
		t.Fatalf("provider = %q, want %q", accounts[0].Provider, store.ExternalProviderGoogleCalendar)
	}
	if accounts[0].Sphere != store.SphereWork {
		t.Fatalf("sphere = %q, want %q", accounts[0].Sphere, store.SphereWork)
	}
}

func TestResolveCalendarSpherePrefersContainerMapping(t *testing.T) {
	st, err := store.New(filepath.Join(t.TempDir(), "slopshell.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })

	if _, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGoogleCalendar, "Family", map[string]any{}); err != nil {
		t.Fatalf("CreateExternalAccount(calendar): %v", err)
	}
	workSphere := store.SphereWork
	if _, err := st.SetContainerMapping(store.ExternalProviderGoogleCalendar, "calendar", "primary", nil, &workSphere); err != nil {
		t.Fatalf("SetContainerMapping(): %v", err)
	}
	accounts, err := GoogleCalendarAccounts(st)
	if err != nil {
		t.Fatalf("GoogleCalendarAccounts() error: %v", err)
	}
	got := ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, "primary", "Family", store.SpherePrivate, accounts)
	if got != store.SphereWork {
		t.Fatalf("ResolveCalendarSphere() = %q, want %q", got, store.SphereWork)
	}
}
