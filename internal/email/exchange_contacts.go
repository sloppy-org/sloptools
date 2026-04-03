package email

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strings"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

type exchangeContactListResponse struct {
	Value    []exchangeContact `json:"value"`
	NextLink string            `json:"@odata.nextLink"`
}

type exchangeContact struct {
	ID             string            `json:"id"`
	DisplayName    string            `json:"displayName"`
	CompanyName    string            `json:"companyName"`
	EmailAddresses []exchangeAddress `json:"emailAddresses"`
	BusinessPhones []string          `json:"businessPhones"`
}

type exchangeAddress struct {
	Name    string `json:"name"`
	Address string `json:"address"`
}

func (c *ExchangeClient) ListContacts(ctx context.Context) ([]providerdata.Contact, error) {
	query := url.Values{
		"$select": {"id,displayName,companyName,emailAddresses,businessPhones"},
		"$top":    {"200"},
	}
	nextURL := c.cfg.BaseURL + "/v1.0/me/contacts?" + query.Encode()
	var out []providerdata.Contact
	for strings.TrimSpace(nextURL) != "" {
		var resp exchangeContactListResponse
		if err := c.doAbsoluteJSON(ctx, http.MethodGet, nextURL, nil, &resp); err != nil {
			return nil, err
		}
		for _, contact := range resp.Value {
			parsed := parseExchangeContact(contact)
			if parsed == nil {
				continue
			}
			out = append(out, *parsed)
		}
		nextURL = strings.TrimSpace(resp.NextLink)
	}
	return out, nil
}

func parseExchangeContact(contact exchangeContact) *providerdata.Contact {
	emailAddress := ""
	for _, address := range contact.EmailAddresses {
		if clean := strings.ToLower(strings.TrimSpace(address.Address)); clean != "" {
			emailAddress = clean
			break
		}
	}
	name := strings.TrimSpace(contact.DisplayName)
	if name == "" {
		name = emailAddress
	}
	if name == "" && emailAddress == "" {
		return nil
	}
	phones := make([]string, 0, len(contact.BusinessPhones))
	for _, phone := range contact.BusinessPhones {
		if clean := strings.TrimSpace(phone); clean != "" {
			phones = append(phones, clean)
		}
	}
	return &providerdata.Contact{
		ProviderRef:  strings.TrimSpace(contact.ID),
		Name:         name,
		Email:        emailAddress,
		Organization: strings.TrimSpace(contact.CompanyName),
		Phones:       phones,
	}
}

func (c *ExchangeClient) doAbsoluteJSON(ctx context.Context, method, fullURL string, reqBody any, respBody any) error {
	token, err := c.ensureAccessToken(ctx)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, method, fullURL, nil)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/json")
	resp, err := c.httpClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		var graphErr exchangeGraphError
		if err := json.NewDecoder(resp.Body).Decode(&graphErr); err == nil && graphErr.Error.Message != "" {
			return fmt.Errorf("exchange graph %s: %s", graphErr.Error.Code, graphErr.Error.Message)
		}
		return fmt.Errorf("exchange graph http %d", resp.StatusCode)
	}
	if respBody == nil || resp.StatusCode == http.StatusNoContent {
		return nil
	}
	if err := json.NewDecoder(resp.Body).Decode(respBody); err != nil {
		return fmt.Errorf("decode exchange response: %w", err)
	}
	return nil
}
