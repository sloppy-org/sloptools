package mailboxsettings

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func newFakeEWSProvider(t *testing.T, mailbox string, handler http.HandlerFunc) (*EWSProvider, func()) {
	t.Helper()
	server := httptest.NewServer(handler)
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: mailbox, Password: "secret"})
	if err != nil {
		server.Close()
		t.Fatalf("ews.NewClient: %v", err)
	}
	provider := NewEWSProvider(client, mailbox)
	cleanup := func() {
		_ = client.Close()
		server.Close()
	}
	return provider, cleanup
}

const oofGetScheduledResponse = `<?xml version="1.0" encoding="utf-8"?>
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
        <InternalReply><Message>Internal reply text</Message></InternalReply>
        <ExternalReply><Message>External reply text</Message></ExternalReply>
      </OofSettings>
      <AllowExternalOof>Known</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`

const oofGetDisabledResponse = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
      <OofSettings xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
        <OofState>Disabled</OofState>
        <ExternalAudience>None</ExternalAudience>
        <InternalReply><Message></Message></InternalReply>
        <ExternalReply><Message></Message></ExternalReply>
      </OofSettings>
      <AllowExternalOof>All</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`

const oofSetSuccessResponse = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <SetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
    </SetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`

func TestEWSProviderGetOOFMapsScheduledResponse(t *testing.T) {
	var requestBody string
	provider, cleanup := newFakeEWSProvider(t, "albert@tugraz.at", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		requestBody = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, oofGetScheduledResponse)
	})
	defer cleanup()

	got, err := provider.GetOOF(t.Context())
	if err != nil {
		t.Fatalf("GetOOF: %v", err)
	}
	if !got.Enabled {
		t.Fatal("Enabled = false, want true (Scheduled state should be enabled)")
	}
	if got.Scope != "contacts" {
		t.Fatalf("Scope = %q, want contacts", got.Scope)
	}
	if got.InternalReply != "Internal reply text" {
		t.Fatalf("InternalReply = %q", got.InternalReply)
	}
	if got.ExternalReply != "External reply text" {
		t.Fatalf("ExternalReply = %q", got.ExternalReply)
	}
	wantStart := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	wantEnd := time.Date(2026, time.May, 1, 17, 0, 0, 0, time.UTC)
	if got.StartAt == nil || !got.StartAt.Equal(wantStart) {
		t.Fatalf("StartAt = %v, want %v", got.StartAt, wantStart)
	}
	if got.EndAt == nil || !got.EndAt.Equal(wantEnd) {
		t.Fatalf("EndAt = %v, want %v", got.EndAt, wantEnd)
	}
	if !strings.Contains(requestBody, "<t:Address>albert@tugraz.at</t:Address>") {
		t.Fatalf("request body missing mailbox address:\n%s", requestBody)
	}
}

func TestEWSProviderGetOOFMapsDisabledResponse(t *testing.T) {
	provider, cleanup := newFakeEWSProvider(t, "ada@example.com", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, oofGetDisabledResponse)
	})
	defer cleanup()

	got, err := provider.GetOOF(t.Context())
	if err != nil {
		t.Fatalf("GetOOF: %v", err)
	}
	if got.Enabled {
		t.Fatal("Enabled = true, want false")
	}
	if got.StartAt != nil || got.EndAt != nil {
		t.Fatalf("StartAt/EndAt = %v/%v, want nil", got.StartAt, got.EndAt)
	}
	if got.Scope != "internal" {
		t.Fatalf("Scope = %q, want internal", got.Scope)
	}
}

func TestEWSProviderSetOOFEnabledBuildsRequest(t *testing.T) {
	var body string
	provider, cleanup := newFakeEWSProvider(t, "albert@tugraz.at", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, oofSetSuccessResponse)
	})
	defer cleanup()

	err := provider.SetOOF(t.Context(), providerdata.OOFSettings{
		Enabled:       true,
		Scope:         "all",
		InternalReply: "Out today",
		ExternalReply: "External: out today",
	})
	if err != nil {
		t.Fatalf("SetOOF: %v", err)
	}
	for _, snippet := range []string{
		`<m:SetUserOofSettingsRequest>`,
		`<t:Address>albert@tugraz.at</t:Address>`,
		`<t:OofState>Enabled</t:OofState>`,
		`<t:ExternalAudience>All</t:ExternalAudience>`,
		`<t:InternalReply><t:Message>Out today</t:Message></t:InternalReply>`,
		`<t:ExternalReply><t:Message>External: out today</t:Message></t:ExternalReply>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
	if strings.Contains(body, `<t:Duration>`) {
		t.Fatalf("request body unexpectedly emitted Duration when not Scheduled:\n%s", body)
	}
}

func TestEWSProviderSetOOFScheduledEmitsDuration(t *testing.T) {
	var body string
	provider, cleanup := newFakeEWSProvider(t, "albert@tugraz.at", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, oofSetSuccessResponse)
	})
	defer cleanup()

	start := time.Date(2026, time.April, 24, 8, 0, 0, 0, time.UTC)
	end := time.Date(2026, time.May, 1, 17, 0, 0, 0, time.UTC)
	err := provider.SetOOF(t.Context(), providerdata.OOFSettings{
		Enabled:       true,
		Scope:         "contacts",
		InternalReply: "Away",
		ExternalReply: "Away (external)",
		StartAt:       &start,
		EndAt:         &end,
	})
	if err != nil {
		t.Fatalf("SetOOF: %v", err)
	}
	for _, snippet := range []string{
		`<t:OofState>Scheduled</t:OofState>`,
		`<t:ExternalAudience>Known</t:ExternalAudience>`,
		`<t:Duration><t:StartTime>2026-04-24T08:00:00Z</t:StartTime><t:EndTime>2026-05-01T17:00:00Z</t:EndTime></t:Duration>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestEWSProviderSetOOFDisabledClearsResponder(t *testing.T) {
	var body string
	provider, cleanup := newFakeEWSProvider(t, "ada@example.com", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, oofSetSuccessResponse)
	})
	defer cleanup()

	if err := provider.SetOOF(t.Context(), providerdata.OOFSettings{Enabled: false, Scope: "internal"}); err != nil {
		t.Fatalf("SetOOF: %v", err)
	}
	if !strings.Contains(body, `<t:OofState>Disabled</t:OofState>`) {
		t.Fatalf("request body missing Disabled state:\n%s", body)
	}
	if !strings.Contains(body, `<t:ExternalAudience>None</t:ExternalAudience>`) {
		t.Fatalf("request body missing audience None:\n%s", body)
	}
}

func TestEWSProviderRoundTripGetSetGet(t *testing.T) {
	state := struct {
		stored ews.OofSettings
		seeded bool
	}{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		switch {
		case strings.Contains(string(data), "GetUserOofSettingsRequest"):
			if !state.seeded {
				_, _ = io.WriteString(w, oofGetDisabledResponse)
				return
			}
			_, _ = io.WriteString(w, renderGetResponse(state.stored))
		case strings.Contains(string(data), "SetUserOofSettingsRequest"):
			state.stored = parseSetRequest(string(data))
			state.seeded = true
			_, _ = io.WriteString(w, oofSetSuccessResponse)
		default:
			t.Fatalf("unexpected EWS request:\n%s", data)
		}
	}))
	defer server.Close()
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: "round-trip", Password: "secret"})
	if err != nil {
		t.Fatalf("ews.NewClient: %v", err)
	}
	defer client.Close()
	provider := NewEWSProvider(client, "round-trip@example.com")

	first, err := provider.GetOOF(t.Context())
	if err != nil {
		t.Fatalf("GetOOF#1: %v", err)
	}
	if first.Enabled {
		t.Fatal("first GetOOF Enabled = true, want false")
	}

	if err := provider.SetOOF(t.Context(), providerdata.OOFSettings{
		Enabled:       true,
		Scope:         "all",
		InternalReply: "round-trip internal",
		ExternalReply: "round-trip external",
	}); err != nil {
		t.Fatalf("SetOOF: %v", err)
	}

	second, err := provider.GetOOF(t.Context())
	if err != nil {
		t.Fatalf("GetOOF#2: %v", err)
	}
	if !second.Enabled {
		t.Fatal("second GetOOF Enabled = false, want true after SetOOF")
	}
	if second.Scope != "all" {
		t.Fatalf("second GetOOF Scope = %q, want all", second.Scope)
	}
	if second.InternalReply != "round-trip internal" {
		t.Fatalf("second GetOOF InternalReply = %q", second.InternalReply)
	}
	if second.ExternalReply != "round-trip external" {
		t.Fatalf("second GetOOF ExternalReply = %q", second.ExternalReply)
	}
}

func TestEWSProviderGetOOFRequiresMailbox(t *testing.T) {
	provider := NewEWSProvider(nil, "")
	if _, err := provider.GetOOF(t.Context()); err == nil {
		t.Fatal("GetOOF() error = nil, want client-not-configured")
	}
}

func TestEWSProviderProviderName(t *testing.T) {
	provider := NewEWSProvider(nil, "x")
	if got := provider.ProviderName(); got != "exchange_ews_mailbox_settings" {
		t.Fatalf("ProviderName() = %q", got)
	}
	if err := provider.Close(); err != nil {
		t.Fatalf("Close() = %v", err)
	}
}

func renderGetResponse(s ews.OofSettings) string {
	stateStr := string(s.State)
	if stateStr == "" {
		stateStr = string(ews.OofStateDisabled)
	}
	audienceStr := string(s.ExternalAudience)
	if audienceStr == "" {
		audienceStr = string(ews.OofAudienceNone)
	}
	duration := ""
	if s.State == ews.OofStateScheduled && !s.Start.IsZero() && !s.End.IsZero() {
		duration = `<Duration><StartTime>` + s.Start.UTC().Format(time.RFC3339) + `</StartTime><EndTime>` + s.End.UTC().Format(time.RFC3339) + `</EndTime></Duration>`
	}
	return `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetUserOofSettingsResponse xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseMessage ResponseClass="Success">
        <ResponseCode>NoError</ResponseCode>
      </ResponseMessage>
      <OofSettings xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
        <OofState>` + stateStr + `</OofState>
        <ExternalAudience>` + audienceStr + `</ExternalAudience>
        ` + duration + `
        <InternalReply><Message>` + s.InternalReply + `</Message></InternalReply>
        <ExternalReply><Message>` + s.ExternalReply + `</Message></ExternalReply>
      </OofSettings>
      <AllowExternalOof>All</AllowExternalOof>
    </GetUserOofSettingsResponse>
  </soap:Body>
</soap:Envelope>`
}

func parseSetRequest(body string) ews.OofSettings {
	out := ews.OofSettings{
		State:            ews.OofStateDisabled,
		ExternalAudience: ews.OofAudienceNone,
	}
	if v, ok := extractTag(body, "OofState"); ok {
		out.State = ews.OofState(v)
	}
	if v, ok := extractTag(body, "ExternalAudience"); ok {
		out.ExternalAudience = ews.OofExternalAudience(v)
	}
	if internal, ok := extractInnerMessage(body, "InternalReply"); ok {
		out.InternalReply = internal
	}
	if external, ok := extractInnerMessage(body, "ExternalReply"); ok {
		out.ExternalReply = external
	}
	if start, ok := extractTag(body, "StartTime"); ok {
		if parsed, err := time.Parse(time.RFC3339, start); err == nil {
			out.Start = parsed
		}
	}
	if end, ok := extractTag(body, "EndTime"); ok {
		if parsed, err := time.Parse(time.RFC3339, end); err == nil {
			out.End = parsed
		}
	}
	return out
}

func extractTag(body, name string) (string, bool) {
	open := "<t:" + name + ">"
	close := "</t:" + name + ">"
	start := strings.Index(body, open)
	if start < 0 {
		return "", false
	}
	start += len(open)
	end := strings.Index(body[start:], close)
	if end < 0 {
		return "", false
	}
	return body[start : start+end], true
}

func extractInnerMessage(body, parent string) (string, bool) {
	openParent := "<t:" + parent + ">"
	closeParent := "</t:" + parent + ">"
	start := strings.Index(body, openParent)
	if start < 0 {
		return "", false
	}
	end := strings.Index(body[start:], closeParent)
	if end < 0 {
		return "", false
	}
	section := body[start : start+end]
	const openMsg = "<t:Message>"
	const closeMsg = "</t:Message>"
	mStart := strings.Index(section, openMsg)
	if mStart < 0 {
		return "", false
	}
	mStart += len(openMsg)
	mEnd := strings.Index(section[mStart:], closeMsg)
	if mEnd < 0 {
		return "", false
	}
	return section[mStart : mStart+mEnd], true
}
