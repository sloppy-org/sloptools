package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/mcp/gtdfocus"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func TestContactSearchSurfacesUnsupportedAsCapabilityCode(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &readOnlyContactsProvider{name: "read_only_contacts"}, nil
	}
	got, err := s.callTool("contact_search", map[string]interface{}{"account_id": account.ID, "query": "alice"})
	if err != nil {
		t.Fatalf("contact_search: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "contacts.Searcher" {
		t.Fatalf("capability = %v, want contacts.Searcher", got["capability"])
	}
}

func TestBrainGTDFocusUsesSloptoolsStoreAndTrackLabels(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "commitments", "compiler.md"), `---
kind: commitment
sphere: work
title: Fix parser
status: next
labels:
  - track/software-compilers
---
# Fix parser
`)
	st, err := store.New(filepath.Join(t.TempDir(), "sloppy.db"))
	if err != nil {
		t.Fatalf("store.New: %v", err)
	}
	t.Cleanup(func() { _ = st.Close() })
	s := NewServerWithStore(t.TempDir(), st)

	tracks, err := s.callTool("brain.gtd.tracks", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.tracks: %v", err)
	}
	if tracks["canonical"] != "labels" || tracks["count"].(int) != 1 {
		t.Fatalf("tracks = %#v, want one canonical label track", tracks)
	}

	focus, err := s.callTool("brain.gtd.focus", map[string]interface{}{
		"sphere":       "work",
		"track":        "software-compilers",
		"project_path": "brain/commitments/compiler.md",
	})
	if err != nil {
		t.Fatalf("brain.gtd.focus: %v", err)
	}
	state := focus["focus"].(gtdfocus.State)
	if state.Track != "software-compilers" || state.Project.Ref != "brain/commitments/compiler.md" {
		t.Fatalf("focus state = %#v", state)
	}
}

func TestContactCreateOnReadOnlyProviderReturnsCapabilityUnsupported(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &readOnlyContactsProvider{name: "read_only_contacts"}, nil
	}
	got, err := s.callTool("contact_create", map[string]interface{}{"account_id": account.ID, "contact": map[string]interface{}{"name": "Anon"}})
	if err != nil {
		t.Fatalf("contact_create: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "contacts.Mutator" {
		t.Fatalf("capability = %v, want contacts.Mutator", got["capability"])
	}
}

func TestContactSearchSurfacesProviderUnsupportedError(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &fakeContactsProvider{name: "exchange_ews_contacts", failSearchWith: fmt.Errorf("ews contacts search: %w", contacts.ErrUnsupported)}, nil
	}
	got, err := s.callTool("contact_search", map[string]interface{}{"account_id": account.ID, "query": "alice"})
	if err != nil {
		t.Fatalf("contact_search: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

func TestContactGroupListSurfacesUnsupportedForBackend(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &readOnlyContactsProvider{name: "exchange_ews_contacts"}, nil
	}
	got, err := s.callTool("contact_group_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("contact_group_list: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "contacts.Grouper" {
		t.Fatalf("capability = %v, want contacts.Grouper", got["capability"])
	}
}

func TestContactGroupListReturnsGroupsForGoogleLikeProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Personal Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeContactsProvider{name: "google_contacts", groups: []contacts.Group{{ID: "contactGroups/family", Name: "Family", MemberCount: 4}, {ID: "contactGroups/friends", Name: "Friends", MemberCount: 12}}}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return provider, nil
	}
	got, err := s.callTool("contact_group_list", map[string]interface{}{"account_id": account.ID})
	if err != nil {
		t.Fatalf("contact_group_list: %v", err)
	}
	groups, ok := got["groups"].([]map[string]interface{})
	if !ok || len(groups) != 2 {
		t.Fatalf("groups = %v, want length 2", got["groups"])
	}
	names := []string{stringValue(t, groups[0]["name"]), stringValue(t, groups[1]["name"])}
	sort.Strings(names)
	if names[0] != "Family" || names[1] != "Friends" {
		t.Fatalf("group names = %v, want [Family Friends]", names)
	}
}

func TestContactPhotoGetReturnsBase64ForCapableProvider(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Personal Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	want := []byte{0x89, 0x50, 0x4e, 0x47, 0x0d, 0x0a, 0x1a, 0x0a}
	provider := &fakeContactsProvider{name: "google_contacts", photoMime: "image/png", photoBytes: want}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return provider, nil
	}
	got, err := s.callTool("contact_photo_get", map[string]interface{}{"account_id": account.ID, "id": "people/c1"})
	if err != nil {
		t.Fatalf("contact_photo_get: %v", err)
	}
	if got["mime"] != "image/png" {
		t.Fatalf("mime = %v, want image/png", got["mime"])
	}
	if got["size_bytes"] != len(want) {
		t.Fatalf("size_bytes = %v, want %d", got["size_bytes"], len(want))
	}
	encoded := stringValue(t, got["data_base64"])
	if encoded == "" {
		t.Fatal("data_base64 missing")
	}
}

func TestContactPhotoGetSurfacesUnsupportedForBackend(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SphereWork, store.ExternalProviderExchangeEWS, "TU Graz", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &readOnlyContactsProvider{name: "exchange_ews_contacts"}, nil
	}
	got, err := s.callTool("contact_photo_get", map[string]interface{}{"account_id": account.ID, "id": "ews:1"})
	if err != nil {
		t.Fatalf("contact_photo_get: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
	if got["capability"] != "contacts.PhotoFetcher" {
		t.Fatalf("capability = %v, want contacts.PhotoFetcher", got["capability"])
	}
}

func TestContactPhotoGetMissingPhotoSurfacesUnsupported(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Personal Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return &fakeContactsProvider{name: "google_contacts", failPhotoWith: fmt.Errorf("contact %q has no photo: %w", "people/c1", contacts.ErrUnsupported)}, nil
	}
	got, err := s.callTool("contact_photo_get", map[string]interface{}{"account_id": account.ID, "id": "people/c1"})
	if err != nil {
		t.Fatalf("contact_photo_get: %v", err)
	}
	if got["error_code"] != "capability_unsupported" {
		t.Fatalf("error_code = %v, want capability_unsupported", got["error_code"])
	}
}

func TestContactToolsAreRegisteredInToolDefinitions(t *testing.T) {
	defs := toolDefinitions()
	want := []string{"contact_list", "contact_get", "contact_search", "contact_create", "contact_update", "contact_delete", "contact_group_list", "contact_photo_get"}
	have := map[string]bool{}
	for _, d := range defs {
		name, _ := d["name"].(string)
		have[name] = true
	}
	for _, name := range want {
		if !have[name] {
			t.Fatalf("tool %q is not registered in toolDefinitions()", name)
		}
	}
}

func TestContactSearchProviderTimingMaintained(t *testing.T) {
	s, st, _ := newDomainServerForTest(t)
	account, err := st.CreateExternalAccount(store.SpherePrivate, store.ExternalProviderGmail, "Personal Gmail", map[string]any{})
	if err != nil {
		t.Fatalf("CreateExternalAccount: %v", err)
	}
	provider := &fakeContactsProvider{
		name:     "google_contacts",
		searched: []providerdata.Contact{{ProviderRef: "people/c42", Name: "Albert Einstein", Email: "ae@example.com"}},
	}
	s.newContactsProvider = func(_ context.Context, _ store.ExternalAccount) (contacts.Provider, error) {
		return provider, nil
	}
	start := time.Now()
	got, err := s.callTool("contact_search", map[string]interface{}{"account_id": account.ID, "query": "albert"})
	if err != nil {
		t.Fatalf("contact_search: %v", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("contact_search took too long: %s", elapsed)
	}
	if provider.searchCalls != 1 {
		t.Fatalf("searchCalls = %d, want 1", provider.searchCalls)
	}
	if got["count"].(int) != 1 {
		t.Fatalf("count = %v, want 1", got["count"])
	}
	results, _ := got["contacts"].([]map[string]interface{})
	if len(results) != 1 || results[0]["name"] != "Albert Einstein" {
		t.Fatalf("search result = %v", results)
	}
}

func TestContactDispatchUnknownToolRouteFailsGracefully(t *testing.T) {
	s, _, _ := newDomainServerForTest(t)
	_, err := s.callTool("contact_unknown", map[string]interface{}{})
	if err == nil {
		t.Fatal("unknown contact tool should return error")
	}
	if !strings.Contains(err.Error(), "contact_unknown") {
		t.Fatalf("error = %v, want mention of contact_unknown", err)
	}
}

// TestBrainGTDReviewListSurfacesDelegatedBucket verifies that markdown
// commitments with the first-class `delegated` status surface in the
// `delegated` queue (separate from `waiting`) and that the new
// `queue_counts` map exposes the bucket count for downstream review tools.
// Surfaced from issue #91 (Manager's-Path delegation).
func TestBrainGTDReviewListSurfacesDelegatedBucket(t *testing.T) {
	tmp := t.TempDir()
	configPath := writeMCPBrainConfig(t, tmp)
	writeMCPBrainFile(t, filepath.Join(tmp, "work", "brain", "gtd", "delegate.md"), `---
kind: commitment
sphere: work
title: Hand off CI rollout
status: delegated
context: meeting
outcome: Hand off CI rollout
delegated_to: Ada Lovelace
follow_up: 2026-05-15
source_bindings:
  - provider: meetings
    ref: 2026-05-01-standup
---
# Hand off CI rollout

## Summary
Owned by Ada from this Friday.

## Next Action
- [ ] Ada drives rollout.

## Evidence
- meetings:2026-05-01-standup

## Linked Items
- None.

## Review Notes
- None.
`)
	s := NewServer(t.TempDir())
	got, err := s.callTool("brain.gtd.review_list", map[string]interface{}{"config_path": configPath, "sphere": "work"})
	if err != nil {
		t.Fatalf("brain.gtd.review_list: %v", err)
	}
	counts, _ := got["queue_counts"].(map[string]int)
	if counts["delegated"] != 1 {
		t.Fatalf("delegated bucket = %d, want 1: %#v", counts["delegated"], counts)
	}
	if counts["waiting"] != 0 {
		t.Fatalf("waiting bucket = %d, want 0; delegated must not double-count: %#v", counts["waiting"], counts)
	}
	items, _ := got["items"].([]gtdReviewItem)
	if len(items) != 1 {
		t.Fatalf("items = %#v, want exactly the delegated commitment", items)
	}
	delegated := items[0]
	if delegated.Queue != "delegated" || delegated.Status != "delegated" {
		t.Fatalf("delegated item queue/status = %q/%q", delegated.Queue, delegated.Status)
	}
	if delegated.DelegatedTo != "Ada Lovelace" || delegated.Actor != "Ada Lovelace" {
		t.Fatalf("delegated item person fields = delegated_to=%q actor=%q", delegated.DelegatedTo, delegated.Actor)
	}
	if delegated.FollowUp != "2026-05-15" {
		t.Fatalf("delegated follow_up = %q, want 2026-05-15", delegated.FollowUp)
	}
}
