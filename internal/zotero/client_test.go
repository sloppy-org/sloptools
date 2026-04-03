package zotero

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)

	client, err := NewClient(
		"12345",
		"api-key-1",
		WithBaseURL(server.URL),
		WithHTTPClient(server.Client()),
	)
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	return client
}

func TestAPIKeyEnvVarAndNewClientFromEnv(t *testing.T) {
	if got, want := APIKeyEnvVar("Main Library"), "SLOPSHELL_ZOTERO_API_KEY_MAIN_LIBRARY"; got != want {
		t.Fatalf("APIKeyEnvVar() = %q, want %q", got, want)
	}

	t.Setenv(APIKeyEnvVar("Main Library"), "token-xyz")

	client, err := NewClientFromEnv("Main Library", "42")
	if err != nil {
		t.Fatalf("NewClientFromEnv() error: %v", err)
	}
	if client.apiKey != "token-xyz" {
		t.Fatalf("apiKey = %q, want token-xyz", client.apiKey)
	}

	if _, err := NewClientFromEnv("Missing", "42"); !errors.Is(err, ErrAPIKeyNotConfigured) {
		t.Fatalf("NewClientFromEnv(missing) error = %v, want ErrAPIKeyNotConfigured", err)
	}
}

func TestListItemsAndCollections(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Zotero-API-Key"); got != "api-key-1" {
			t.Fatalf("Zotero-API-Key = %q, want api-key-1", got)
		}
		switch r.URL.Path {
		case "/users/12345/items":
			if got := r.URL.Query().Get("collection"); got != "COLL1" {
				t.Fatalf("collection = %q, want COLL1", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"key":     "ITEM1",
				"version": 7,
				"data": map[string]any{
					"itemType":         "journalArticle",
					"title":            "Pragmatic Testing",
					"DOI":              "10.1000/example",
					"abstractNote":     "Short abstract.",
					"publicationTitle": "Journal of Tests",
					"creators": []map[string]any{{
						"creatorType": "author",
						"firstName":   "Ada",
						"lastName":    "Lovelace",
					}},
					"tags": []map[string]any{{"tag": "ml", "type": 0}},
				},
			}})
		case "/users/12345/collections":
			if got := r.URL.Query().Get("top"); got != "1" {
				t.Fatalf("top = %q, want 1", got)
			}
			_ = json.NewEncoder(w).Encode([]map[string]any{{
				"key":     "COLL1",
				"version": 2,
				"data": map[string]any{
					"name":             "Papers",
					"parentCollection": "",
				},
			}})
		default:
			t.Fatalf("unexpected request %s", r.URL.String())
		}
	})

	items, err := client.ListItems(context.Background(), ListRemoteItemsOptions{
		CollectionKey: "COLL1",
		Limit:         5,
	})
	if err != nil {
		t.Fatalf("ListItems() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items len = %d, want 1", len(items))
	}
	if items[0].Key != "ITEM1" || items[0].Creators[0].LastName != "Lovelace" || items[0].Tags[0].Tag != "ml" {
		t.Fatalf("items[0] = %#v", items[0])
	}

	collections, err := client.ListCollections(context.Background(), ListRemoteCollectionsOptions{
		TopLevelOnly: true,
	})
	if err != nil {
		t.Fatalf("ListCollections() error: %v", err)
	}
	if len(collections) != 1 || collections[0].Key != "COLL1" || collections[0].Name != "Papers" {
		t.Fatalf("collections = %#v", collections)
	}
}

func TestAPIError(t *testing.T) {
	client := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	})

	if _, err := client.ListItems(context.Background(), ListRemoteItemsOptions{}); err == nil {
		t.Fatal("ListItems() error = nil, want APIError")
	} else {
		var apiErr *APIError
		if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusTooManyRequests {
			t.Fatalf("ListItems() error = %v, want APIError 429", err)
		}
	}
}
