package ews

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

func (c *Client) getTypedItems(ctx context.Context, folderID string, offset, max int, expectedLocal string) ([]itemXML, error) {
	found, err := c.FindMessages(ctx, folderID, offset, max)
	if err != nil {
		return nil, err
	}
	if len(found.ItemIDs) == 0 {
		return nil, nil
	}
	var resp getItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody(found.ItemIDs, true), &resp); err != nil {
		return nil, err
	}
	out := make([]itemXML, 0, len(resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values))
	for _, raw := range resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values {
		if strings.EqualFold(raw.XMLName.Local, expectedLocal) {
			out = append(out, raw)
		}
	}
	return out, nil
}

func (c *Client) call(ctx context.Context, soapAction, innerXML string, target any) error {
	return c.callWithHTTPClient(ctx, c.httpClient, soapAction, innerXML, target)
}

func (c *Client) callWithHTTPClient(ctx context.Context, client *http.Client, soapAction, innerXML string, target any) error {
	release, err := c.acquireRequestSlot(ctx, client)
	if err != nil {
		return err
	}
	defer release()
	body := fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:xsi="http://www.w3.org/2001/XMLSchema-instance"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types"
               xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Header><t:RequestServerVersion Version="%s" /></soap:Header>
  <soap:Body>%s</soap:Body>
</soap:Envelope>`, xmlEscapeAttr(c.cfg.ServerVersion), innerXML)
	var sanitized []byte // Exchange sometimes returns a bare 401 on write operations when the
	// cached affinity/NTLM session cookie has expired server-side. The
	// negotiator won't re-handshake without a WWW-Authenticate hint, so
	// drop the session cookies and retry once with a fresh handshake.

	var status int
	for attempt := 0; attempt < 2; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.cfg.Endpoint, strings.NewReader(body))
		if err != nil {
			return err
		}
		req.SetBasicAuth(c.cfg.Username, c.cfg.Password)
		req.Header.Set("Content-Type", "text/xml; charset=utf-8")
		req.Header.Set("Accept", "text/xml")
		req.Header.Set("SOAPAction", fmt.Sprintf(`"http://schemas.microsoft.com/exchange/services/2006/messages/%s"`, soapAction))
		if mailbox := strings.TrimSpace(c.cfg.Username); mailbox != "" {
			req.Header.Set("X-AnchorMailbox", mailbox)
			req.Header.Set("X-PreferServerAffinity", "true")
		}
		resp, err := client.Do(req)
		if err != nil {
			return err
		}
		data, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			return readErr
		}
		sanitized, _ = sanitizeXML10Document(data)
		status = resp.StatusCode
		if status == http.StatusUnauthorized && attempt == 0 {
			c.resetSessionCookies(client)
			continue
		}
		break
	}
	if faultErr := parseSOAPFaultError(soapAction, status, sanitized); faultErr != nil {
		return faultErr
	}
	if status < 200 || status >= 300 {
		return fmt.Errorf("ews http %d: %s", status, strings.TrimSpace(string(sanitized)))
	}
	if err := xml.Unmarshal(sanitized, target); err != nil {
		return fmt.Errorf("decode ews %s response: %w", soapAction, err)
	}
	if rc := responseCode(target); rc != "" && !strings.EqualFold(rc, "NoError") {
		return fmt.Errorf("ews %s: %s", soapAction, rc)
	}
	return nil
}

func (c *Client) resetSessionCookies(client *http.Client) {
	if c == nil || c.session == nil {
		return
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		return
	}
	c.session.mu.Lock()
	c.session.jar = jar
	c.session.mu.Unlock()
	if client != nil {
		client.Jar = jar
	}
	if c.httpClient != nil && c.httpClient != client {
		c.httpClient.Jar = jar
	}
	if c.streamingHTTPClient != nil && c.streamingHTTPClient != client {
		c.streamingHTTPClient.Jar = jar
	}
} // resetSessionCookies drops the cached cookie jar shared across all clients
// for this mailbox so the next request re-runs the NTLM handshake. Only the
// passed-in http.Client's Jar field is swapped; peer clients that still point
// at the previous jar will pick up the fresh jar on their next call because
// the shared session holds the canonical reference.

func (c *Client) acquireRequestSlot(ctx context.Context, client *http.Client) (func(), error) {
	if c == nil || c.session == nil || client == nil || client == c.streamingHTTPClient {
		return func() {
		}, nil
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case c.session.requestGate <- struct{}{}:
		return func() {
			select {
			case <-c.session.requestGate:
			default:
			}
		}, nil
	}
}

type soapFaultEnvelope struct {
	Body struct {
		Fault struct {
			FaultCode   string `xml:"faultcode"`
			FaultString string `xml:"faultstring"`
			Detail      struct {
				ResponseCode string `xml:"ResponseCode"`
				Message      string `xml:"Message"`
				MessageXML   struct {
					Values []soapFaultMessageXMLValue `xml:"Value"`
				} `xml:"MessageXml"`
			} `xml:"detail"`
		} `xml:"Fault"`
	} `xml:"Body"`
}

type soapFaultMessageXMLValue struct {
	Name  string `xml:"Name,attr"`
	Value string `xml:",chardata"`
}

func parseSOAPFaultError(operation string, statusCode int, data []byte) error {
	if !bytes.Contains(data, []byte("<Fault")) && !bytes.Contains(data, []byte(":Fault")) {
		return nil
	}
	var env soapFaultEnvelope
	if err := xml.Unmarshal(data, &env); err != nil {
		return &SOAPFaultError{Operation: operation, StatusCode: statusCode, Message: strings.TrimSpace(string(data))}
	}
	fault := env.Body.Fault
	message := strings.TrimSpace(fault.Detail.Message)
	if message == "" {
		message = strings.TrimSpace(fault.FaultString)
	}
	responseCode := strings.TrimSpace(fault.Detail.ResponseCode)
	if strings.EqualFold(responseCode, "ErrorServerBusy") {
		backoff := parseSOAPFaultBackoff(fault.Detail.MessageXML.Values)
		return &BackoffError{Operation: operation, ResponseCode: responseCode, Message: message, Backoff: backoff}
	}
	return &SOAPFaultError{Operation: operation, StatusCode: statusCode, FaultCode: strings.TrimSpace(fault.FaultCode), ResponseCode: responseCode, Message: message}
}

func parseSOAPFaultBackoff(values []soapFaultMessageXMLValue) time.Duration {
	for _, value := range values {
		if !strings.EqualFold(strings.TrimSpace(value.Name), "BackOffMilliseconds") {
			continue
		}
		ms, err := strconv.Atoi(strings.TrimSpace(value.Value))
		if err != nil || ms <= 0 {
			return 0
		}
		return time.Duration(ms) * time.Millisecond
	}
	return 0
}

func sanitizeXML10Document(data []byte) ([]byte, int) {
	if len(data) == 0 {
		return data, 0
	}
	original := data
	removed := 0
	data, refsRemoved := sanitizeIllegalXML10EntityRefs(data)
	removed += refsRemoved
	var out bytes.Buffer
	out.Grow(len(data))
	for len(data) > 0 {
		r, size := utf8.DecodeRune(data)
		if r == utf8.RuneError && size == 1 {
			removed++
			data = data[1:]
			continue
		}
		if isXML10Rune(r) {
			out.WriteRune(r)
		} else {
			removed++
		}
		data = data[size:]
	}
	if removed == 0 {
		return original, 0
	}
	return out.Bytes(), removed
}

func sanitizeIllegalXML10EntityRefs(data []byte) ([]byte, int) {
	removed := 0
	sanitized := xmlNumericEntityPattern.ReplaceAllFunc(data, func(match []byte) []byte {
		groups := xmlNumericEntityPattern.FindSubmatch(match)
		if len(groups) != 3 {
			return match
		}
		base := 10
		raw := groups[2]
		if len(groups[1]) > 0 {
			base = 16
			raw = groups[1]
		}
		value, err := strconv.ParseInt(string(raw), base, 32)
		if err != nil {
			removed++
			return nil
		}
		if isXML10Rune(rune(value)) {
			return match
		}
		removed++
		return nil
	})
	return sanitized, removed
}

func isXML10Rune(r rune) bool {
	switch {
	case r == 0x9 || r == 0xA || r == 0xD:
		return true
	case r >= 0x20 && r <= 0xD7FF:
		return true
	case r >= 0xE000 && r <= 0xFFFD:
		return true
	case r >= 0x10000 && r <= 0x10FFFF:
		return true
	default:
		return false
	}
}

func (c *Client) subscribeStreaming(ctx context.Context, opts WatchOptions) (string, error) {
	body := streamingSubscribeBody(opts)
	var resp subscribeEnvelope
	if err := c.call(ctx, "Subscribe", body, &resp); err != nil {
		return "", err
	}
	return strings.TrimSpace(resp.Body.SubscribeResponse.ResponseMessages.Message.SubscriptionID), nil
}

func (c *Client) getStreamingEvents(ctx context.Context, subscriptionID string, connectionTimeout int) (StreamBatch, error) {
	body := fmt.Sprintf(`<m:GetStreamingEvents><m:SubscriptionIds><t:SubscriptionId>%s</t:SubscriptionId></m:SubscriptionIds><m:ConnectionTimeout>%d</m:ConnectionTimeout></m:GetStreamingEvents>`, xmlEscapeText(strings.TrimSpace(subscriptionID)), connectionTimeout)
	var resp getStreamingEventsEnvelope
	if err := c.callWithHTTPClient(ctx, c.streamingHTTPClient, "GetStreamingEvents", body, &resp); err != nil {
		return StreamBatch{}, err
	}
	return resp.Body.GetStreamingEventsResponse.ResponseMessages.Message.toBatch(), nil
}

func getItemBody(ids []string, includeBody bool) string {
	var b strings.Builder
	b.WriteString(`<m:GetItem><m:ItemShape>`)
	b.WriteString(`<t:BaseShape>AllProperties</t:BaseShape><t:BodyType>Text</t:BodyType>`)
	b.WriteString(`</m:ItemShape><m:ItemIds>`)
	for _, id := range ids {
		b.WriteString(`<t:ItemId Id="`)
		b.WriteString(xmlEscapeAttr(id))
		b.WriteString(`" />`)
	}
	b.WriteString(`</m:ItemIds></m:GetItem>`)
	return b.String()
}

func getAttachmentBody(attachmentID string) string {
	var b strings.Builder
	b.WriteString(`<m:GetAttachment><m:AttachmentIds><t:AttachmentId Id="`)
	b.WriteString(xmlEscapeAttr(strings.TrimSpace(attachmentID)))
	b.WriteString(`" /></m:AttachmentIds></m:GetAttachment>`)
	return b.String()
}

func draftMessageXML(message DraftMessage) string {
	encoded := base64.StdEncoding.EncodeToString(message.MIME)
	var b strings.Builder
	b.WriteString(`<t:Message>`)
	if strings.TrimSpace(message.Subject) != "" {
		b.WriteString(`<t:Subject>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(message.Subject)))
		b.WriteString(`</t:Subject>`)
	}
	if encoded != "" {
		b.WriteString(`<t:MimeContent CharacterSet="UTF-8">`)
		b.WriteString(encoded)
		b.WriteString(`</t:MimeContent>`)
	}
	b.WriteString(`</t:Message>`)
	return b.String()
}

func setDraftMimeContentXML(message DraftMessage) string {
	encoded := base64.StdEncoding.EncodeToString(message.MIME)
	var b strings.Builder
	if strings.TrimSpace(message.Subject) != "" {
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:Subject" /><t:Message><t:Subject>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(message.Subject)))
		b.WriteString(`</t:Subject></t:Message></t:SetItemField>`)
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:MimeContent" /><t:Message><t:MimeContent CharacterSet="UTF-8">`)
	b.WriteString(encoded)
	b.WriteString(`</t:MimeContent></t:Message></t:SetItemField>`)
	return b.String()
}

func folderIDXML(folderID string) string {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return `<t:DistinguishedFolderId Id="inbox" />`
	}
	if canonical, ok := canonicalDistinguishedFolderID(folderID); ok {
		return fmt.Sprintf(`<t:DistinguishedFolderId Id="%s" />`, xmlEscapeAttr(canonical))
	}
	return fmt.Sprintf(`<t:FolderId Id="%s" />`, xmlEscapeAttr(folderID))
}

func folderRefXML(folderID string) string {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return ""
	}
	if canonical, ok := canonicalDistinguishedFolderID(folderID); ok {
		return fmt.Sprintf(`<t:DistinguishedFolderId Id="%s" />`, xmlEscapeAttr(canonical))
	}
	return fmt.Sprintf(`<t:FolderId Id="%s" />`, xmlEscapeAttr(folderID))
}

func streamingConnectionTimeoutMinutes(value time.Duration) int {
	if value <= 0 {
		return 29
	}
	minutes := int(value / time.Minute)
	if minutes <= 0 {
		minutes = 1
	}
	if minutes > 30 {
		minutes = 30
	}
	return minutes
}

func streamingSubscribeBody(opts WatchOptions) string {
	subscribeAll := opts.SubscribeToAllFolders || len(opts.FolderIDs) == 0
	var b strings.Builder
	b.WriteString(`<m:Subscribe><m:StreamingSubscriptionRequest`)
	if subscribeAll {
		b.WriteString(` SubscribeToAllFolders="true"`)
	}
	b.WriteString(`>`)
	if !subscribeAll {
		b.WriteString(`<m:FolderIds>`)
		for _, folderID := range compactStrings(opts.FolderIDs) {
			b.WriteString(folderIDXML(folderID))
		}
		b.WriteString(`</m:FolderIds>`)
	}
	b.WriteString(`<t:EventTypes>`)
	for _, eventType := range []string{"CopiedEvent", "CreatedEvent", "DeletedEvent", "FreeBusyChangedEvent", "ModifiedEvent", "MovedEvent", "NewMailEvent"} {
		b.WriteString(`<t:EventType>`)
		b.WriteString(eventType)
		b.WriteString(`</t:EventType>`)
	}
	b.WriteString(`</t:EventTypes></m:StreamingSubscriptionRequest></m:Subscribe>`)
	return b.String()
}

func folderIDOrDistinguished(folderID, fallback string) string {
	if strings.TrimSpace(folderID) != "" {
		return folderID
	}
	return fallback
}

func compactStrings(values []string) []string {
	out := make([]string, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		clean := strings.TrimSpace(value)
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

func itemIDsXML(ids []string) string {
	var b strings.Builder
	for _, id := range ids {
		b.WriteString(`<t:ItemId Id="`)
		b.WriteString(xmlEscapeAttr(id))
		b.WriteString(`" />`)
	}
	return b.String()
}

func maxInt(v, fallback int) int {
	if v > 0 {
		return v
	}
	return fallback
}

func xmlEscapeAttr(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(strings.TrimSpace(value)))
	return strings.ReplaceAll(b.String(), `"`, "&quot;")
}

func xmlEscapeText(value string) string {
	var b bytes.Buffer
	_ = xml.EscapeText(&b, []byte(value))
	return b.String()
}

func parseTime(value string) time.Time {
	value = strings.TrimSpace(value)
	if value == "" {
		return time.Time{}
	}
	for _, layout := range []string{time.RFC3339Nano, time.RFC3339, "2006-01-02T15:04:05", "2006-01-02"} {
		if parsed, err := time.Parse(layout, value); err == nil {
			return parsed.UTC()
		}
	}
	return time.Time{}
}

func parseBool(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "1":
		return true
	default:
		return false
	}
}

func parseInt(value string) int {
	n, _ := strconv.Atoi(strings.TrimSpace(value))
	return n
}
