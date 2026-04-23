package contacts

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/providerdata"
	"google.golang.org/api/option"
	people "google.golang.org/api/people/v1"
)

type providerdataContact = providerdata.Contact

// buildStubProvider returns a GmailProvider whose People API calls hit the
// provided test HTTP handler instead of the real Google endpoint. The
// HTTP handler acts as the People API server so request/response shapes
// stay exercised end to end.
func buildStubProvider(t *testing.T, handler http.HandlerFunc) *GmailProvider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	provider := &GmailProvider{}
	provider.svcFn = func(ctx context.Context) (*people.Service, error) {
		return people.NewService(ctx, option.WithoutAuthentication(), option.WithEndpoint(srv.URL))
	}
	return provider
}

func TestListContactsPaginatesAndMapsFullFields(t *testing.T) {
	calls := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/people/me/connections") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		calls++
		if calls == 1 {
			if r.URL.Query().Get("pageToken") != "" {
				t.Fatalf("first call should have empty pageToken, got %q", r.URL.Query().Get("pageToken"))
			}
			writeJSON(w, map[string]any{
				"connections": []any{
					map[string]any{
						"resourceName":   "people/c1",
						"names":          []any{map[string]any{"displayName": "Ada Lovelace"}},
						"emailAddresses": []any{map[string]any{"value": "ada@example.com"}},
						"organizations":  []any{map[string]any{"name": "Analytical Engine"}},
						"phoneNumbers":   []any{map[string]any{"value": "+11111"}},
						"addresses": []any{map[string]any{
							"streetAddress": "1 Programmer Lane", "city": "London", "postalCode": "W1", "country": "UK", "type": "home",
						}},
						"biographies": []any{map[string]any{"value": "Notes about Ada"}},
						"photos":      []any{map[string]any{"url": "https://example.invalid/photo.jpg"}},
					},
				},
				"nextPageToken": "next",
			})
			return
		}
		if r.URL.Query().Get("pageToken") != "next" {
			t.Fatalf("second call pageToken = %q, want next", r.URL.Query().Get("pageToken"))
		}
		writeJSON(w, map[string]any{
			"connections": []any{
				map[string]any{
					"resourceName":   "people/c2",
					"names":          []any{map[string]any{"displayName": "Carol"}},
					"emailAddresses": []any{map[string]any{"value": "CAROL@example.com"}},
				},
			},
		})
	}
	provider := buildStubProvider(t, handler)

	out, err := provider.ListContacts(context.Background())
	if err != nil {
		t.Fatalf("ListContacts: %v", err)
	}
	if len(out) != 2 {
		t.Fatalf("len = %d, want 2", len(out))
	}
	if out[0].Name != "Ada Lovelace" || out[0].Email != "ada@example.com" {
		t.Fatalf("contact[0] = %#v", out[0])
	}
	if out[0].Organization != "Analytical Engine" {
		t.Fatalf("organization = %q", out[0].Organization)
	}
	if len(out[0].Addresses) != 1 || out[0].Addresses[0].City != "London" {
		t.Fatalf("addresses = %#v", out[0].Addresses)
	}
	if out[0].Notes != "Notes about Ada" {
		t.Fatalf("notes = %q", out[0].Notes)
	}
	if len(out[0].Photos) != 1 || out[0].Photos[0].URL == "" {
		t.Fatalf("photos = %#v", out[0].Photos)
	}
	if out[1].Email != "carol@example.com" {
		t.Fatalf("carol email lowercased = %q", out[1].Email)
	}
	if calls != 2 {
		t.Fatalf("calls = %d, want 2", calls)
	}
}

func TestSearchContactsHitsSearchEndpoint(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/people:searchContacts") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		if r.URL.Query().Get("query") != "ada" {
			t.Fatalf("query = %q", r.URL.Query().Get("query"))
		}
		writeJSON(w, map[string]any{
			"results": []any{
				map[string]any{
					"person": map[string]any{
						"resourceName":   "people/c1",
						"names":          []any{map[string]any{"displayName": "Ada"}},
						"emailAddresses": []any{map[string]any{"value": "ada@example.com"}},
					},
				},
			},
		})
	}
	provider := buildStubProvider(t, handler)

	out, err := provider.SearchContacts(context.Background(), "ada")
	if err != nil {
		t.Fatalf("SearchContacts: %v", err)
	}
	if len(out) != 1 || out[0].Email != "ada@example.com" {
		t.Fatalf("out = %#v", out)
	}
}

func TestCreateContactWritesPerson(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/v1/people:createContact") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var in people.Person
		if err := json.Unmarshal(body, &in); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(in.EmailAddresses) != 1 || in.EmailAddresses[0].Value != "new@example.com" {
			t.Fatalf("emails = %#v", in.EmailAddresses)
		}
		writeJSON(w, map[string]any{
			"resourceName":   "people/c99",
			"names":          []any{map[string]any{"displayName": "New"}},
			"emailAddresses": in.EmailAddresses,
		})
	}
	provider := buildStubProvider(t, handler)

	got, err := provider.CreateContact(context.Background(), googleTestContact("New", "new@example.com"))
	if err != nil {
		t.Fatalf("CreateContact: %v", err)
	}
	if got.ProviderRef != "people/c99" {
		t.Fatalf("ProviderRef = %q", got.ProviderRef)
	}
}

func TestUpdateContactFetchesEtagBeforePatching(t *testing.T) {
	gets := 0
	patches := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.Contains(r.URL.Path, "/v1/people/c1"):
			gets++
			writeJSON(w, map[string]any{
				"resourceName":   "people/c1",
				"etag":           "etag-123",
				"names":          []any{map[string]any{"displayName": "Old"}},
				"emailAddresses": []any{map[string]any{"value": "old@example.com"}},
			})
		case r.Method == http.MethodPatch && strings.Contains(r.URL.Path, "/v1/people/c1:updateContact"):
			patches++
			body, _ := io.ReadAll(r.Body)
			var in people.Person
			if err := json.Unmarshal(body, &in); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if in.Etag != "etag-123" {
				t.Fatalf("etag = %q, want etag-123", in.Etag)
			}
			writeJSON(w, map[string]any{
				"resourceName":   "people/c1",
				"names":          []any{map[string]any{"displayName": "Updated"}},
				"emailAddresses": []any{map[string]any{"value": "updated@example.com"}},
			})
		default:
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
	}
	provider := buildStubProvider(t, handler)

	c := googleTestContact("Updated", "updated@example.com")
	c.ProviderRef = "people/c1"
	got, err := provider.UpdateContact(context.Background(), c)
	if err != nil {
		t.Fatalf("UpdateContact: %v", err)
	}
	if got.Name != "Updated" {
		t.Fatalf("name = %q", got.Name)
	}
	if gets != 1 || patches != 1 {
		t.Fatalf("gets=%d patches=%d", gets, patches)
	}
}

func TestDeleteContactIssuesDeleteRequest(t *testing.T) {
	called := 0
	handler := func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodDelete || !strings.HasSuffix(r.URL.Path, "/v1/people/c1:deleteContact") {
			t.Fatalf("unexpected %s %s", r.Method, r.URL.Path)
		}
		called++
		writeJSON(w, map[string]any{})
	}
	provider := buildStubProvider(t, handler)

	if err := provider.DeleteContact(context.Background(), "people/c1"); err != nil {
		t.Fatalf("DeleteContact: %v", err)
	}
	if called != 1 {
		t.Fatalf("called = %d", called)
	}
}

func TestListContactGroupsMapsFormattedName(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, "/v1/contactGroups") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		writeJSON(w, map[string]any{
			"contactGroups": []any{
				map[string]any{"resourceName": "contactGroups/myContacts", "formattedName": "My Contacts", "memberCount": 10},
			},
		})
	}
	provider := buildStubProvider(t, handler)

	groups, err := provider.ListContactGroups(context.Background())
	if err != nil {
		t.Fatalf("ListContactGroups: %v", err)
	}
	if len(groups) != 1 {
		t.Fatalf("len = %d, want 1", len(groups))
	}
	if groups[0].Name != "My Contacts" || groups[0].MemberCount != 10 {
		t.Fatalf("group = %#v", groups[0])
	}
}

func TestAddToGroupPrefixesResourceNames(t *testing.T) {
	handler := func(w http.ResponseWriter, r *http.Request) {
		if !strings.Contains(r.URL.Path, "/v1/contactGroups/") || !strings.Contains(r.URL.Path, ":modify") {
			t.Fatalf("unexpected path %q", r.URL.Path)
		}
		body, _ := io.ReadAll(r.Body)
		var in people.ModifyContactGroupMembersRequest
		if err := json.Unmarshal(body, &in); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(in.ResourceNamesToAdd) != 2 {
			t.Fatalf("add count = %d", len(in.ResourceNamesToAdd))
		}
		for _, name := range in.ResourceNamesToAdd {
			if !strings.HasPrefix(name, "people/") {
				t.Fatalf("name %q missing people/ prefix", name)
			}
		}
		writeJSON(w, map[string]any{})
	}
	provider := buildStubProvider(t, handler)

	if err := provider.AddToGroup(context.Background(), "contactGroups/g1", []string{"c1", "people/c2"}); err != nil {
		t.Fatalf("AddToGroup: %v", err)
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(v)
}

func googleTestContact(name, email string) providerdataContact {
	return providerdataContact{Name: name, Email: email}
}
