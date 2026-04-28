package todoist

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
)

func listPaginated[T any](ctx context.Context, c *Client, endpoint string, query url.Values) ([]T, error) {
	query = cloneQuery(query)
	query.Set("limit", "200")
	var out []T
	for {
		var raw json.RawMessage
		if err := c.doJSON(ctx, http.MethodGet, endpoint, query, nil, &raw, http.StatusOK); err != nil {
			return nil, err
		}
		var legacy []T
		if err := json.Unmarshal(raw, &legacy); err == nil {
			return legacy, nil
		}
		var page struct {
			Results    []T    `json:"results"`
			NextCursor string `json:"next_cursor"`
		}
		if err := json.Unmarshal(raw, &page); err != nil {
			return nil, err
		}
		out = append(out, page.Results...)
		if strings.TrimSpace(page.NextCursor) == "" {
			return out, nil
		}
		query.Set("cursor", page.NextCursor)
	}
}

func cloneQuery(query url.Values) url.Values {
	out := url.Values{}
	for key, values := range query {
		out[key] = append([]string(nil), values...)
	}
	return out
}
