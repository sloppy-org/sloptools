package mcp

import (
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/groupware"
	inboxpkg "github.com/sloppy-org/sloptools/internal/mcp/inbox"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func (s *Server) contactsProviderForTool(args map[string]interface{}) (store.ExternalAccount, contacts.Provider, error) {
	st, err := s.requireStore()
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	account, err := accountForTool(st, args, "contacts-capable", isContactsCapableProvider)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	provider, err := s.contactsProviderForAccount(context.Background(), account)
	if err != nil {
		return store.ExternalAccount{}, nil, err
	}
	return account, provider, nil
}

func (s *Server) contactsProviderForAccount(ctx context.Context, account store.ExternalAccount) (contacts.Provider, error) {
	if s.newContactsProvider != nil {
		return s.newContactsProvider(ctx, account)
	}
	if s.groupware == nil {
		return nil, errors.New("groupware registry is not configured")
	}
	return s.groupware.ContactsFor(ctx, account.ID)
}

func isContactsCapableProvider(provider string) bool {
	switch strings.ToLower(strings.TrimSpace(provider)) {
	case store.ExternalProviderGmail, store.ExternalProviderGoogleCalendar, store.ExternalProviderExchangeEWS:
		return true
	default:
		return false
	}
}

func firstContactsCapableAccount(st *store.Store, sphere string) (store.ExternalAccount, error) {
	return firstCapableAccount(st, sphere, "contacts-capable", isContactsCapableProvider)
}

func (s *Server) contactList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	items, err := provider.ListContacts(context.Background())
	if err != nil {
		return nil, err
	}
	sort.Slice(items, func(i, j int) bool {
		left := strings.ToLower(strings.TrimSpace(items[i].Name))
		right := strings.ToLower(strings.TrimSpace(items[j].Name))
		if left == right {
			return strings.ToLower(items[i].Email) < strings.ToLower(items[j].Email)
		}
		return left < right
	})
	payloads := make([]map[string]interface{}, 0, len(items))
	for _, contact := range items {
		payloads = append(payloads, contactPayload(contact))
	}
	total := len(payloads)
	offset := intArg(args, "offset", 0)
	if offset < 0 {
		offset = 0
	}
	if offset > total {
		offset = total
	}
	limit := intArg(args, "limit", contactListDefaultLimit)
	if limit <= 0 {
		limit = contactListDefaultLimit
	}
	end := offset + limit
	if end > total {
		end = total
	}
	window := payloads[offset:end]
	out := map[string]interface{}{
		"account_id": account.ID,
		"provider":   provider.ProviderName(),
		"contacts":   window,
		"count":      len(window),
		"total":      total,
		"offset":     offset,
		"limit":      limit,
		"truncated":  offset > 0 || end < total,
	}
	if end < total {
		out["next_offset"] = end
	}
	return out, nil
}

const contactListDefaultLimit = 100

func (s *Server) contactGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	contact, err := provider.GetContact(context.Background(), id)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "contact": contactPayload(contact)}, nil
}

func (s *Server) contactSearch(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	query := strings.TrimSpace(strArg(args, "query"))
	if query == "" {
		return nil, errors.New("query is required")
	}
	searcher, ok := groupware.Supports[contacts.Searcher](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Searcher", "error_detail": fmt.Sprintf("provider %s does not support contact search", provider.ProviderName())}, nil
	}
	items, err := searcher.SearchContacts(context.Background(), query)
	if err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Searcher", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	payloads := make([]map[string]interface{}, 0, len(items))
	for _, contact := range items {
		payloads = append(payloads, contactPayload(contact))
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "query": query, "contacts": payloads, "count": len(payloads)}, nil
}

func (s *Server) contactCreate(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	contact, err := contactFromArgs(args)
	if err != nil {
		return nil, err
	}
	mutator, ok := groupware.Supports[contacts.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": fmt.Sprintf("provider %s does not support contact mutation", provider.ProviderName())}, nil
	}
	created, err := mutator.CreateContact(context.Background(), contact)
	if err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "created": true, "contact": contactPayload(created)}, nil
}

func (s *Server) contactUpdate(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	contact, err := contactFromArgs(args)
	if err != nil {
		return nil, err
	}
	if strings.TrimSpace(contact.ProviderRef) == "" {
		return nil, errors.New("contact.provider_ref is required for update")
	}
	mutator, ok := groupware.Supports[contacts.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": fmt.Sprintf("provider %s does not support contact mutation", provider.ProviderName())}, nil
	}
	updated, err := mutator.UpdateContact(context.Background(), contact)
	if err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "updated": true, "contact": contactPayload(updated)}, nil
}

func (s *Server) contactDelete(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	mutator, ok := groupware.Supports[contacts.Mutator](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": fmt.Sprintf("provider %s does not support contact mutation", provider.ProviderName())}, nil
	}
	if err := mutator.DeleteContact(context.Background(), id); err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Mutator", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "id": id, "deleted": true}, nil
}

func (s *Server) contactGroupList(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	grouper, ok := groupware.Supports[contacts.Grouper](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Grouper", "error_detail": fmt.Sprintf("provider %s does not expose contact groups", provider.ProviderName())}, nil
	}
	groups, err := grouper.ListContactGroups(context.Background())
	if err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.Grouper", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	payloads := make([]map[string]interface{}, 0, len(groups))
	for _, group := range groups {
		payloads = append(payloads, map[string]interface{}{"id": group.ID, "name": group.Name, "member_count": group.MemberCount})
	}
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "groups": payloads, "count": len(payloads)}, nil
}

func (s *Server) contactPhotoGet(args map[string]interface{}) (map[string]interface{}, error) {
	account, provider, err := s.contactsProviderForTool(args)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	id := strings.TrimSpace(strArg(args, "id"))
	if id == "" {
		return nil, errors.New("id is required")
	}
	fetcher, ok := groupware.Supports[contacts.PhotoFetcher](provider)
	if !ok {
		return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.PhotoFetcher", "error_detail": fmt.Sprintf("provider %s does not expose contact photos", provider.ProviderName())}, nil
	}
	data, mimeType, err := fetcher.GetContactPhoto(context.Background(), id)
	if err != nil {
		if errors.Is(err, contacts.ErrUnsupported) {
			return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "error_code": "capability_unsupported", "capability": "contacts.PhotoFetcher", "error_detail": err.Error()}, nil
		}
		return nil, err
	}
	encoded := base64.StdEncoding.EncodeToString(data)
	return map[string]interface{}{"account_id": account.ID, "provider": provider.ProviderName(), "id": id, "mime": strings.TrimSpace(mimeType), "data_base64": encoded, "size_bytes": len(data)}, nil
}

func contactPayload(contact providerdata.Contact) map[string]interface{} {
	addresses := make([]map[string]interface{}, 0, len(contact.Addresses))
	for _, address := range contact.Addresses {
		addresses = append(addresses, map[string]interface{}{"type": address.Type, "street": address.Street, "city": address.City, "region": address.Region, "postal": address.Postal, "country": address.Country})
	}
	photos := make([]map[string]interface{}, 0, len(contact.Photos))
	for _, photo := range contact.Photos {
		entry := map[string]interface{}{"url": photo.URL, "content_type": photo.ContentType}
		if len(photo.Bytes) > 0 {
			entry["data_base64"] = base64.StdEncoding.EncodeToString(photo.Bytes)
			entry["size_bytes"] = len(photo.Bytes)
		}
		photos = append(photos, entry)
	}
	payload := map[string]interface{}{"provider_ref": contact.ProviderRef, "name": contact.Name, "email": contact.Email, "organization": contact.Organization, "phones": append([]string(nil), contact.Phones...), "addresses": addresses, "notes": contact.Notes, "photos": photos}
	if contact.Birthday != nil {
		payload["birthday"] = contact.Birthday.UTC().Format("2006-01-02")
	}
	return payload
}

func contactFromArgs(args map[string]interface{}) (providerdata.Contact, error) {
	raw, ok := args["contact"].(map[string]interface{})
	if !ok || raw == nil {
		return providerdata.Contact{}, errors.New("contact is required")
	}
	contact := providerdata.Contact{
		ProviderRef:  strings.TrimSpace(strArg(raw, "provider_ref")),
		Name:         strings.TrimSpace(strArg(raw, "name")),
		Email:        strings.TrimSpace(strArg(raw, "email")),
		Organization: strings.TrimSpace(strArg(raw, "organization")),
		Notes:        strings.TrimSpace(strArg(raw, "notes")),
	}
	contact.Phones = stringListArg(raw, "phones")
	addresses, err := postalAddressListArg(raw, "addresses")
	if err != nil {
		return providerdata.Contact{}, err
	}
	contact.Addresses = addresses
	if rawBirthday, ok := optionalStringArg(raw, "birthday"); ok && rawBirthday != nil && *rawBirthday != "" {
		parsed, err := parseContactBirthday(*rawBirthday)
		if err != nil {
			return providerdata.Contact{}, err
		}
		bd := parsed
		contact.Birthday = &bd
	}
	if contact.Name == "" && contact.Email == "" && contact.ProviderRef == "" {
		return providerdata.Contact{}, errors.New("contact must include at least one of name, email, or provider_ref")
	}
	return contact, nil
}

func postalAddressListArg(args map[string]interface{}, key string) ([]providerdata.PostalAddress, error) {
	raw, ok := args[key]
	if !ok || raw == nil {
		return nil, nil
	}
	list, ok := raw.([]interface{})
	if !ok {
		return nil, fmt.Errorf("%s must be a list of objects", key)
	}
	out := make([]providerdata.PostalAddress, 0, len(list))
	for i, value := range list {
		entry, ok := value.(map[string]interface{})
		if !ok {
			return nil, fmt.Errorf("%s[%d] must be an object", key, i)
		}
		address := providerdata.PostalAddress{
			Type:    strings.TrimSpace(strArg(entry, "type")),
			Street:  strings.TrimSpace(strArg(entry, "street")),
			City:    strings.TrimSpace(strArg(entry, "city")),
			Region:  strings.TrimSpace(strArg(entry, "region")),
			Postal:  strings.TrimSpace(strArg(entry, "postal")),
			Country: strings.TrimSpace(strArg(entry, "country")),
		}
		if address.Street == "" && address.City == "" && address.Region == "" && address.Postal == "" && address.Country == "" {
			continue
		}
		out = append(out, address)
	}
	return out, nil
}

func parseContactBirthday(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	layouts := []string{time.RFC3339, "2006-01-02"}
	for _, layout := range layouts {
		if value, err := time.Parse(layout, clean); err == nil {
			return time.Date(value.Year(), value.Month(), value.Day(), 0, 0, 0, 0, time.UTC), nil
		}
	}
	return time.Time{}, fmt.Errorf("birthday %q must be RFC3339 or YYYY-MM-DD", raw)
}

func (s *Server) dispatchInbox(method string, args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	handler := inboxpkg.Handler{Store: st, BrainConfigPath: s.brainConfigArg(args), TaskProvider: s.tasksProviderForAccount}
	return handler.Dispatch(method, args)
}
