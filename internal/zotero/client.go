package zotero

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
)

type Client struct {
	baseURL    string
	userID     string
	apiKey     string
	httpClient *http.Client
}

type Option func(*Client)

func WithBaseURL(raw string) Option {
	return func(c *Client) {
		c.baseURL = strings.TrimRight(strings.TrimSpace(raw), "/")
	}
}

func WithHTTPClient(client *http.Client) Option {
	return func(c *Client) {
		c.httpClient = client
	}
}

func NewClient(userID, apiKey string, opts ...Option) (*Client, error) {
	cleanUserID := strings.TrimSpace(userID)
	if cleanUserID == "" {
		return nil, ErrUserIDRequired
	}
	cleanAPIKey := strings.TrimSpace(apiKey)
	if cleanAPIKey == "" {
		return nil, ErrAPIKeyNotConfigured
	}
	client := &Client{
		baseURL:    defaultAPIBaseURL,
		userID:     cleanUserID,
		apiKey:     cleanAPIKey,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		if opt != nil {
			opt(client)
		}
	}
	if client.baseURL == "" {
		client.baseURL = defaultAPIBaseURL
	}
	if client.httpClient == nil {
		client.httpClient = http.DefaultClient
	}
	return client, nil
}

func NewClientFromEnv(label, userID string, opts ...Option) (*Client, error) {
	value, ok := os.LookupEnv(APIKeyEnvVar(label))
	if !ok || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("%w: %s", ErrAPIKeyNotConfigured, APIKeyEnvVar(label))
	}
	return NewClient(userID, value, opts...)
}

func (c *Client) ListItems(ctx context.Context, opts ListRemoteItemsOptions) ([]RemoteItem, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if clean := strings.TrimSpace(opts.CollectionKey); clean != "" {
		query.Set("collection", clean)
	}
	if opts.SinceVersion > 0 {
		query.Set("since", strconv.Itoa(opts.SinceVersion))
	}
	var payload []zoteroAPIEntry
	if err := c.doJSON(ctx, "/users/"+url.PathEscape(c.userID)+"/items", query, &payload); err != nil {
		return nil, err
	}
	items := make([]RemoteItem, 0, len(payload))
	for _, entry := range payload {
		items = append(items, decodeRemoteItem(entry))
	}
	return items, nil
}

func (c *Client) ListCollections(ctx context.Context, opts ListRemoteCollectionsOptions) ([]RemoteCollection, error) {
	query := url.Values{}
	if opts.Limit > 0 {
		query.Set("limit", strconv.Itoa(opts.Limit))
	}
	if opts.TopLevelOnly {
		query.Set("top", "1")
	}
	if opts.SinceVersion > 0 {
		query.Set("since", strconv.Itoa(opts.SinceVersion))
	}
	var payload []zoteroAPIEntry
	if err := c.doJSON(ctx, "/users/"+url.PathEscape(c.userID)+"/collections", query, &payload); err != nil {
		return nil, err
	}
	collections := make([]RemoteCollection, 0, len(payload))
	for _, entry := range payload {
		collections = append(collections, decodeRemoteCollection(entry))
	}
	return collections, nil
}

func (c *Client) doJSON(ctx context.Context, path string, query url.Values, out any) error {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return err
	}
	if query != nil {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return err
	}
	req.Header.Set("Zotero-API-Key", c.apiKey)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		payload, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(payload))}
	}
	return json.NewDecoder(io.LimitReader(resp.Body, 1024*1024)).Decode(out)
}

type zoteroAPIEntry struct {
	Key     string         `json:"key"`
	Version int            `json:"version"`
	Data    map[string]any `json:"data"`
}

func decodeRemoteItem(entry zoteroAPIEntry) RemoteItem {
	item := RemoteItem{
		Key:     strings.TrimSpace(entry.Key),
		Version: entry.Version,
		Raw:     entry.Data,
	}
	data := entry.Data
	item.ItemType = stringValue(data["itemType"])
	item.Title = stringValue(data["title"])
	item.AbstractNote = stringValue(data["abstractNote"])
	item.DOI = stringValue(data["DOI"])
	item.ISBN = stringValue(data["ISBN"])
	item.Date = stringValue(data["date"])
	item.PublicTitle = stringValue(data["publicationTitle"])
	item.ParentItem = stringValue(data["parentItem"])
	if rawCreators, ok := data["creators"].([]any); ok {
		item.Creators = make([]RemoteCreator, 0, len(rawCreators))
		for _, raw := range rawCreators {
			creatorMap, _ := raw.(map[string]any)
			item.Creators = append(item.Creators, RemoteCreator{
				CreatorType: stringValue(creatorMap["creatorType"]),
				FirstName:   stringValue(creatorMap["firstName"]),
				LastName:    stringValue(creatorMap["lastName"]),
				Name:        stringValue(creatorMap["name"]),
			})
		}
	}
	if rawTags, ok := data["tags"].([]any); ok {
		item.Tags = make([]RemoteTag, 0, len(rawTags))
		for _, raw := range rawTags {
			tagMap, _ := raw.(map[string]any)
			item.Tags = append(item.Tags, RemoteTag{
				Tag:  stringValue(tagMap["tag"]),
				Type: intValue(tagMap["type"]),
			})
		}
	}
	return item
}

func decodeRemoteCollection(entry zoteroAPIEntry) RemoteCollection {
	data := entry.Data
	return RemoteCollection{
		Key:       strings.TrimSpace(entry.Key),
		Version:   entry.Version,
		Name:      stringValue(data["name"]),
		ParentKey: stringValue(data["parentCollection"]),
		Raw:       data,
	}
}

func stringValue(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func intValue(v any) int {
	switch typed := v.(type) {
	case float64:
		return int(typed)
	case int:
		return typed
	case int64:
		return int(typed)
	case json.Number:
		n, _ := typed.Int64()
		return int(n)
	default:
		return 0
	}
}
