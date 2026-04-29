package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGetUserOofSettingsParsesDisabledResponse(t *testing.T) {
	var (
		body         string
		soapAction   string
		seenRequests int
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenRequests++
		soapAction = r.Header.Get("SOAPAction")
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
      <OofSettings xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
        <OofState>Disabled</OofState>
        <ExternalAudience>None</ExternalAudience>
        <Duration>
          <StartTime>2026-01-01T00:00:00Z</StartTime>
          <EndTime>2026-01-08T00:00:00Z</EndTime>
        </Duration>
        <InternalReply>
          <Message></Message>
        </InternalReply>
        <ExternalReply>
          <Message></Message>
        </ExternalReply>
      </OofSettings>
      <AllowExternalOof>All</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetUserOofSettings(t.Context(), "ada@example.com")
	if err != nil {
		t.Fatalf("GetUserOofSettings() error: %v", err)
	}
	if seenRequests != 1 {
		t.Fatalf("requests = %d, want 1", seenRequests)
	}
	if got.State != OofStateDisabled {
		t.Fatalf("State = %q, want %q", got.State, OofStateDisabled)
	}
	if got.ExternalAudience != OofAudienceNone {
		t.Fatalf("ExternalAudience = %q, want %q", got.ExternalAudience, OofAudienceNone)
	}
	if got.InternalReply != "" || got.ExternalReply != "" {
		t.Fatalf("reply bodies = %q/%q, want empty", got.InternalReply, got.ExternalReply)
	}
	if !strings.Contains(soapAction, "GetUserOofSettings") {
		t.Fatalf("SOAPAction = %q, want GetUserOofSettings", soapAction)
	}
	for _, snippet := range []string{
		`<m:GetUserOofSettingsRequest>`,
		`<t:Mailbox><t:Address>ada@example.com</t:Address></t:Mailbox>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientGetUserOofSettingsParsesScheduledResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
      <OofSettings xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
        <OofState>Scheduled</OofState>
        <ExternalAudience>Known</ExternalAudience>
        <Duration>
          <StartTime>2026-04-24T08:00:00Z</StartTime>
          <EndTime>2026-05-01T17:00:00Z</EndTime>
        </Duration>
        <InternalReply>
          <Message>I am away until 2026-05-01.</Message>
        </InternalReply>
        <ExternalReply>
          <Message>Thanks for your message. I am out of office.</Message>
        </ExternalReply>
      </OofSettings>
      <AllowExternalOof>Known</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-2", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetUserOofSettings(t.Context(), "ada@example.com")
	if err != nil {
		t.Fatalf("GetUserOofSettings() error: %v", err)
	}
	if got.State != OofStateScheduled {
		t.Fatalf("State = %q, want %q", got.State, OofStateScheduled)
	}
	if got.ExternalAudience != OofAudienceKnown {
		t.Fatalf("ExternalAudience = %q, want %q", got.ExternalAudience, OofAudienceKnown)
	}
	wantStart := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.May, 1, 17, 0, 0, 0, time.UTC)
	if !got.Start.Equal(wantStart) {
		t.Fatalf("Start = %v, want %v", got.Start, wantStart)
	}
	if !got.End.Equal(wantEnd) {
		t.Fatalf("End = %v, want %v", got.End, wantEnd)
	}
	if got.InternalReply != "I am away until 2026-05-01." {
		t.Fatalf("InternalReply = %q", got.InternalReply)
	}
	if got.ExternalReply != "Thanks for your message. I am out of office." {
		t.Fatalf("ExternalReply = %q", got.ExternalReply)
	}
}

func TestClientGetUserOofSettingsParsesEnabledResponseWithoutDuration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
      <OofSettings xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
        <OofState>Enabled</OofState>
        <ExternalAudience>All</ExternalAudience>
        <InternalReply>
          <Message>Internal text</Message>
        </InternalReply>
        <ExternalReply>
          <Message>External text</Message>
        </ExternalReply>
      </OofSettings>
      <AllowExternalOof>All</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-3", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetUserOofSettings(t.Context(), "ada@example.com")
	if err != nil {
		t.Fatalf("GetUserOofSettings() error: %v", err)
	}
	if got.State != OofStateEnabled {
		t.Fatalf("State = %q, want %q", got.State, OofStateEnabled)
	}
	if !got.Start.IsZero() || !got.End.IsZero() {
		t.Fatalf("Duration = %v..%v, want zero", got.Start, got.End)
	}
}

func TestClientGetUserOofSettingsRequiresMailbox(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "https://exchange.example", Username: "ert-oof-4", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	if _, err := client.GetUserOofSettings(t.Context(), "  "); err == nil {
		t.Fatal("GetUserOofSettings() error = nil, want missing-mailbox error")
	}
}

func TestClientSetUserOofSettingsBuildsScheduledRequest(t *testing.T) {
	var (
		body       string
		soapAction string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <SetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
    </SetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-5", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	settings := OofSettings{
		State:            OofStateScheduled,
		ExternalAudience: OofAudienceAll,
		Start:            time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC),
		End:              time.Date(2026, time.May, 1, 17, 0, 0, 0, time.UTC),
		InternalReply:    "Out at conference & offline.",
		ExternalReply:    "External: please email back next week.",
	}
	if err := client.SetUserOofSettings(t.Context(), "ada@example.com", settings); err != nil {
		t.Fatalf("SetUserOofSettings() error: %v", err)
	}
	if !strings.Contains(soapAction, "SetUserOofSettings") {
		t.Fatalf("SOAPAction = %q, want SetUserOofSettings", soapAction)
	}
	for _, snippet := range []string{
		`<m:SetUserOofSettingsRequest>`,
		`<t:Mailbox><t:Address>ada@example.com</t:Address></t:Mailbox>`,
		`<t:UserOofSettings>`,
		`<t:OofState>Scheduled</t:OofState>`,
		`<t:ExternalAudience>All</t:ExternalAudience>`,
		`<t:Duration><t:StartTime>2026-04-24T08:00:00Z</t:StartTime><t:EndTime>2026-05-01T17:00:00Z</t:EndTime></t:Duration>`,
		`<t:InternalReply><t:Message>Out at conference &amp; offline.</t:Message></t:InternalReply>`,
		`<t:ExternalReply><t:Message>External: please email back next week.</t:Message></t:ExternalReply>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientSetUserOofSettingsOmitsDurationWhenNotScheduled(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <SetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
    </SetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-6", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	if err := client.SetUserOofSettings(t.Context(), "ada@example.com", OofSettings{
		State:            OofStateDisabled,
		ExternalAudience: OofAudienceNone,
	}); err != nil {
		t.Fatalf("SetUserOofSettings() error: %v", err)
	}
	if !strings.Contains(body, `<t:OofState>Disabled</t:OofState>`) {
		t.Fatalf("request body missing Disabled state:\n%s", body)
	}
	if strings.Contains(body, `<t:Duration>`) {
		t.Fatalf("request body unexpectedly emitted Duration when Disabled:\n%s", body)
	}
}

func TestClientSetUserOofSettingsSurfacesErrorResponseCode(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <SetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Error">
        <ResponseCode>ErrorAccessDenied</ResponseCode>
        <MessageText>Access is denied.</MessageText>
      </ResponseMessage>
    </SetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert-oof-7", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.SetUserOofSettings(t.Context(), "ada@example.com", OofSettings{State: OofStateDisabled})
	if err == nil {
		t.Fatal("SetUserOofSettings() error = nil, want ErrorAccessDenied")
	}
	if !strings.Contains(err.Error(), "ErrorAccessDenied") {
		t.Fatalf("error = %v, want ErrorAccessDenied", err)
	}
}

func TestClientSetUserOofSettingsRequiresMailbox(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "https://exchange.example", Username: "ert-oof-8", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	if err := client.SetUserOofSettings(t.Context(), "  ", OofSettings{State: OofStateDisabled}); err == nil {
		t.Fatal("SetUserOofSettings() error = nil, want missing-mailbox error")
	}
}
