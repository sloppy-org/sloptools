package zotero

import (
	"bufio"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func (r *Reader) AttachmentFilePath(attachment Attachment) string {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(path), "storage:") {
		relative := strings.TrimPrefix(path, "storage:")
		relative = strings.TrimLeft(relative, "/\\")
		if relative == "" {
			return ""
		}
		return filepath.Join(filepath.Dir(r.path), "storage", strings.TrimSpace(attachment.Key), filepath.FromSlash(relative))
	}
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		parsed, err := url.Parse(path)
		if err != nil {
			return ""
		}
		return parsed.Path
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(path)
}

func (r *Reader) AttachmentFileURL(attachment Attachment) string {
	path := r.AttachmentFilePath(attachment)
	if path == "" {
		return ""
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}

var (
	latexCitationPattern  = regexp.MustCompile(`\\cite[a-zA-Z*]*\{([^}]+)\}`)
	pandocCitationPattern = regexp.MustCompile(`\[[^\]]*@([^\]]+)\]`)
	pandocKeyPattern      = regexp.MustCompile(`@([A-Za-z0-9:_./-]+)`)
)

func ExtractCitationKeys(text string) []string {
	seen := map[string]struct{}{}
	out := make([]string, 0, 4)
	appendKey := func(raw string) {
		key := strings.TrimSpace(strings.TrimPrefix(raw, "@"))
		if key == "" {
			return
		}
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, key)
	}
	for _, match := range latexCitationPattern.FindAllStringSubmatch(text, -1) {
		for _, raw := range strings.Split(match[1], ",") {
			appendKey(raw)
		}
	}
	for _, match := range pandocCitationPattern.FindAllStringSubmatch(text, -1) {
		for _, keyMatch := range pandocKeyPattern.FindAllStringSubmatch(match[0], -1) {
			appendKey(keyMatch[1])
		}
	}
	return out
}

func (r *Reader) ResolveItemsByCitationKey(ctx context.Context, keys []string) ([]Item, error) {
	if len(keys) == 0 {
		return nil, nil
	}
	items, err := r.ListItems(ctx)
	if err != nil {
		return nil, err
	}
	byKey := make(map[string]Item, len(items))
	for _, item := range items {
		key := strings.TrimSpace(item.CitationKey)
		if key == "" {
			continue
		}
		byKey[key] = item
	}
	out := make([]Item, 0, len(keys))
	for _, key := range keys {
		item, ok := byKey[strings.TrimSpace(key)]
		if !ok {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (r *Reader) ResolveCitationText(ctx context.Context, text string) ([]Item, error) {
	return r.ResolveItemsByCitationKey(ctx, ExtractCitationKeys(text))
}

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
	client := &Client{baseURL: defaultAPIBaseURL, userID: cleanUserID, apiKey: cleanAPIKey, httpClient: http.DefaultClient}
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
	item := RemoteItem{Key: strings.TrimSpace(entry.Key), Version: entry.Version, Raw: entry.Data}
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
			item.Creators = append(item.Creators, RemoteCreator{CreatorType: stringValue(creatorMap["creatorType"]), FirstName: stringValue(creatorMap["firstName"]), LastName: stringValue(creatorMap["lastName"]), Name: stringValue(creatorMap["name"])})
		}
	}
	if rawTags, ok := data["tags"].([]any); ok {
		item.Tags = make([]RemoteTag, 0, len(rawTags))
		for _, raw := range rawTags {
			tagMap, _ := raw.(map[string]any)
			item.Tags = append(item.Tags, RemoteTag{Tag: stringValue(tagMap["tag"]), Type: intValue(tagMap["type"])})
		}
	}
	return item
}

func decodeRemoteCollection(entry zoteroAPIEntry) RemoteCollection {
	data := entry.Data
	return RemoteCollection{Key: strings.TrimSpace(entry.Key), Version: entry.Version, Name: stringValue(data["name"]), ParentKey: stringValue(data["parentCollection"]), Raw: data}
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

type Reader struct {
	db   *sql.DB
	path string
}

func OpenReader(path string) (*Reader, error) {
	clean := filepath.Clean(strings.TrimSpace(path))
	if clean == "" {
		return nil, ErrDatabaseNotFound
	}
	if _, err := os.Stat(clean); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrDatabaseNotFound
		}
		return nil, err
	}
	db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(clean)+"?mode=ro")
	if err != nil {
		return nil, err
	}
	return &Reader{db: db, path: clean}, nil
}

func OpenDefaultReader(home string) (*Reader, error) {
	path, err := FindDefaultDatabase(home)
	if err != nil {
		return nil, err
	}
	return OpenReader(path)
}

func (r *Reader) Close() error {
	if r == nil || r.db == nil {
		return nil
	}
	return r.db.Close()
}

func FindDefaultDatabase(home string) (string, error) {
	root, err := resolveHome(home)
	if err != nil {
		return "", err
	}
	for _, candidate := range candidateDatabasePaths(root) {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return filepath.Clean(candidate), nil
		}
	}
	return "", ErrDatabaseNotFound
}

func candidateDatabasePaths(home string) []string {
	var out []string
	macPath := filepath.Join(home, "Zotero", "zotero.sqlite")
	out = append(out, macPath)
	base := filepath.Join(home, ".zotero", "zotero")
	if profilePath := parseDefaultProfile(base); profilePath != "" {
		out = append(out, filepath.Join(base, profilePath, "zotero.sqlite"))
	}
	if matches, err := filepath.Glob(filepath.Join(base, "*", "zotero.sqlite")); err == nil {
		slices.Sort(matches)
		out = append(out, matches...)
	}
	return uniqueCleanPaths(out)
}

func resolveHome(home string) (string, error) {
	if strings.TrimSpace(home) != "" {
		return filepath.Clean(home), nil
	}
	root, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Clean(root), nil
}

func parseDefaultProfile(base string) string {
	f, err := os.Open(filepath.Join(base, "profiles.ini"))
	if err != nil {
		return ""
	}
	defer f.Close()
	var path string
	isDefault := false
	flush := func() string {
		if isDefault && strings.TrimSpace(path) != "" {
			return path
		}
		return ""
	}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		switch {
		case line == "":
		case strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]"):
			if found := flush(); found != "" {
				return found
			}
			path = ""
			isDefault = false
		case strings.HasPrefix(line, "Path="):
			path = strings.TrimSpace(strings.TrimPrefix(line, "Path="))
		case strings.HasPrefix(line, "Default="):
			isDefault = strings.TrimSpace(strings.TrimPrefix(line, "Default=")) == "1"
		}
	}
	return flush()
}

func uniqueCleanPaths(paths []string) []string {
	seen := make(map[string]struct{}, len(paths))
	out := make([]string, 0, len(paths))
	for _, path := range paths {
		clean := filepath.Clean(strings.TrimSpace(path))
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

func (r *Reader) ListCollections(ctx context.Context) ([]Collection, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT c.collectionID, c.key, c.collectionName, COALESCE(parent.key, '')
		FROM collections c
		LEFT JOIN collections parent ON parent.collectionID = c.parentCollectionID
		ORDER BY lower(c.collectionName), c.collectionID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Collection
	for rows.Next() {
		var item Collection
		if err := rows.Scan(&item.ID, &item.Key, &item.Name, &item.ParentKey); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}

func (r *Reader) ListTags(ctx context.Context) ([]Tag, error) {
	rows, err := r.db.QueryContext(ctx, `
		SELECT t.name, COUNT(it.itemID)
		FROM tags t
		LEFT JOIN itemTags it ON it.tagID = t.tagID
		GROUP BY t.tagID, t.name
		ORDER BY lower(t.name), t.tagID`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Tag
	for rows.Next() {
		var item Tag
		if err := rows.Scan(&item.Name, &item.ItemCount); err != nil {
			return nil, err
		}
		out = append(out, item)
	}
	return out, rows.Err()
}
