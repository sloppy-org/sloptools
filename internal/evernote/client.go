package evernote

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

type Client struct {
	baseURL    string
	token      string
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

func NewClient(token string, opts ...Option) (*Client, error) {
	cleanToken := strings.TrimSpace(token)
	if cleanToken == "" {
		return nil, ErrTokenNotConfigured
	}
	client := &Client{
		baseURL:    defaultAPIBaseURL,
		token:      cleanToken,
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

func NewClientFromEnv(label string, opts ...Option) (*Client, error) {
	value, ok := os.LookupEnv(TokenEnvVar(label))
	if !ok || strings.TrimSpace(value) == "" {
		return nil, fmt.Errorf("%w: %s", ErrTokenNotConfigured, TokenEnvVar(label))
	}
	return NewClient(value, opts...)
}

func (c *Client) ListNotebooks(ctx context.Context) ([]Notebook, error) {
	body, err := c.get(ctx, "/notebooks", nil)
	if err != nil {
		return nil, err
	}
	payloads, err := decodeEnvelopeSlice[notebookPayload](body, "notebooks")
	if err != nil {
		return nil, err
	}
	notebooks := make([]Notebook, 0, len(payloads))
	for _, payload := range payloads {
		notebooks = append(notebooks, decodeNotebook(payload))
	}
	return notebooks, nil
}

func (c *Client) ListNotes(ctx context.Context, notebookID string, opts ListNotesOptions) ([]NoteSummary, error) {
	query := url.Values{}
	if clean := strings.TrimSpace(notebookID); clean != "" {
		query.Set("notebook_id", clean)
	}
	if clean := strings.TrimSpace(opts.Query); clean != "" {
		query.Set("query", clean)
	}
	if clean := strings.TrimSpace(opts.Tag); clean != "" {
		query.Set("tag", clean)
	}
	if clean := strings.TrimSpace(opts.UpdatedAfter); clean != "" {
		query.Set("updated_after", clean)
	}
	if opts.Limit > 0 {
		query.Set("limit", fmt.Sprintf("%d", opts.Limit))
	}
	if opts.Offset > 0 {
		query.Set("offset", fmt.Sprintf("%d", opts.Offset))
	}
	body, err := c.get(ctx, "/notes", query)
	if err != nil {
		return nil, err
	}
	payloads, err := decodeEnvelopeSlice[notePayload](body, "notes")
	if err != nil {
		return nil, err
	}
	notes := make([]NoteSummary, 0, len(payloads))
	for _, payload := range payloads {
		notes = append(notes, decodeNoteSummary(payload))
	}
	return notes, nil
}

func (c *Client) GetNote(ctx context.Context, id string) (Note, error) {
	noteID := strings.TrimSpace(id)
	if noteID == "" {
		return Note{}, ErrNoteIDRequired
	}
	body, err := c.get(ctx, "/notes/"+url.PathEscape(noteID), nil)
	if err != nil {
		return Note{}, err
	}
	payload, err := decodeEnvelopeValue[notePayload](body, "note")
	if err != nil {
		return Note{}, err
	}
	return decodeNote(payload), nil
}

func (c *Client) ListTags(ctx context.Context) ([]Tag, error) {
	body, err := c.get(ctx, "/tags", nil)
	if err != nil {
		return nil, err
	}
	payloads, err := decodeEnvelopeSlice[tagPayload](body, "tags")
	if err != nil {
		return nil, err
	}
	tags := make([]Tag, 0, len(payloads))
	for _, payload := range payloads {
		tags = append(tags, decodeTag(payload))
	}
	return tags, nil
}

func (c *Client) get(ctx context.Context, path string, query url.Values) ([]byte, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	u, err := url.Parse(c.baseURL + path)
	if err != nil {
		return nil, err
	}
	if len(query) > 0 {
		u.RawQuery = query.Encode()
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+c.token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, 2*1024*1024))
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, &APIError{StatusCode: resp.StatusCode, Body: strings.TrimSpace(string(body))}
	}
	return body, nil
}

func decodeEnvelopeSlice[T any](body []byte, field string) ([]T, error) {
	var direct []T
	if err := json.Unmarshal(body, &direct); err == nil {
		return direct, nil
	}
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err != nil {
		return nil, err
	}
	payload := envelope[field]
	if len(payload) == 0 {
		return []T{}, nil
	}
	if err := json.Unmarshal(payload, &direct); err != nil {
		return nil, err
	}
	return direct, nil
}

func decodeEnvelopeValue[T any](body []byte, field string) (T, error) {
	var direct T
	var envelope map[string]json.RawMessage
	if err := json.Unmarshal(body, &envelope); err == nil {
		if payload := envelope[field]; len(payload) > 0 {
			if err := json.Unmarshal(payload, &direct); err != nil {
				return direct, err
			}
			return direct, nil
		}
	}
	if err := json.Unmarshal(body, &direct); err != nil {
		return direct, err
	}
	return direct, nil
}

func (p *notebookPayload) UnmarshalJSON(data []byte) error {
	type alias notebookPayload
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	raw, err := mustObject(data)
	if err != nil {
		return err
	}
	*p = notebookPayload(decoded)
	p.Raw = raw
	return nil
}

func (p *notePayload) UnmarshalJSON(data []byte) error {
	type alias notePayload
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	raw, err := mustObject(data)
	if err != nil {
		return err
	}
	*p = notePayload(decoded)
	p.Raw = raw
	return nil
}

func (p *tagPayload) UnmarshalJSON(data []byte) error {
	type alias tagPayload
	var decoded alias
	if err := json.Unmarshal(data, &decoded); err != nil {
		return err
	}
	raw, err := mustObject(data)
	if err != nil {
		return err
	}
	*p = tagPayload(decoded)
	p.Raw = raw
	return nil
}

func decodeNotebook(payload notebookPayload) Notebook {
	return Notebook{
		ID:        firstNonEmpty(payload.ID, payload.GUID),
		Name:      strings.TrimSpace(payload.Name),
		Stack:     strings.TrimSpace(payload.Stack),
		UpdatedAt: firstNonEmpty(payload.UpdatedAt, payload.UpdatedTS),
		Raw:       payload.Raw,
	}
}

func decodeNoteSummary(payload notePayload) NoteSummary {
	text, _, _ := ConvertENMLToText(firstNonEmpty(payload.ContentENML, payload.ENML, payload.Content))
	return NoteSummary{
		ID:          firstNonEmpty(payload.ID, payload.GUID),
		NotebookID:  firstNonEmpty(payload.NotebookID, payload.NotebookGUID),
		Title:       strings.TrimSpace(payload.Title),
		UpdatedAt:   firstNonEmpty(payload.UpdatedAt, payload.UpdatedTS),
		CreatedAt:   firstNonEmpty(payload.CreatedAt, payload.CreatedTS),
		TagNames:    append([]string(nil), payload.TagNames...),
		ContentText: text,
		Raw:         payload.Raw,
	}
}

func decodeNote(payload notePayload) Note {
	enml := firstNonEmpty(payload.ContentENML, payload.ENML, payload.Content)
	text, markdown, tasks := ConvertENMLToText(enml)
	return Note{
		ID:              firstNonEmpty(payload.ID, payload.GUID),
		NotebookID:      firstNonEmpty(payload.NotebookID, payload.NotebookGUID),
		Title:           strings.TrimSpace(payload.Title),
		CreatedAt:       firstNonEmpty(payload.CreatedAt, payload.CreatedTS),
		UpdatedAt:       firstNonEmpty(payload.UpdatedAt, payload.UpdatedTS),
		TagNames:        append([]string(nil), payload.TagNames...),
		ContentENML:     enml,
		ContentText:     text,
		ContentMarkdown: markdown,
		Tasks:           tasks,
		Raw:             payload.Raw,
	}
}

func decodeTag(payload tagPayload) Tag {
	return Tag{
		ID:       firstNonEmpty(payload.ID, payload.GUID),
		Name:     strings.TrimSpace(payload.Name),
		ParentID: firstNonEmpty(payload.ParentID, payload.ParentGUID),
		Raw:      payload.Raw,
	}
}
