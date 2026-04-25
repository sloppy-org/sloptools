package calendar

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/ews"
	"github.com/sloppy-org/sloptools/internal/providerdata"
)

func TestEWSProviderProviderName(t *testing.T) {
	p := NewEWSProvider(nil)
	if p.ProviderName() != ProviderNameEWS {
		t.Fatalf("ProviderName() = %q, want %q", p.ProviderName(), ProviderNameEWS)
	}
}

func TestEWSProviderListCalendars(t *testing.T) {
	p := NewEWSProvider(nil)
	cals, err := p.ListCalendars(context.Background())
	if err != nil {
		t.Fatalf("ListCalendars() error: %v", err)
	}
	if len(cals) != 1 {
		t.Fatalf("ListCalendars() returned %d calendars, want 1", len(cals))
	}
	if cals[0].ID != string(ews.FolderKindCalendar) {
		t.Fatalf("Calendar ID = %q, want %q", cals[0].ID, ews.FolderKindCalendar)
	}
	if !cals[0].Primary {
		t.Fatalf("Primary = false, want true")
	}
}

func TestEWSProviderListEventsMapsAttendees(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindItemResponse>
      <m:ResponseMessages>
        <m:FindItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder IndexedPagingOffset="0" TotalItemsInView="1" IncludesLastItemInRange="true">
            <t:CalendarItems>
              <t:CalendarItem>
                <t:ItemId Id="ev-1" ChangeKey="ck-1" />
                <t:Subject>Meeting</t:Subject>
                <t:Start>2026-05-01T10:00:00Z</t:Start>
                <t:End>2026-05-01T11:00:00Z</t:End>
                <t:UID>uid-1@test.com</t:UID>
                <t:Organizer><t:Mailbox><t:Name>Org</t:Name><t:EmailAddress>org@test.com</t:EmailAddress></t:Mailbox></t:Organizer>
                <t:RequiredAttendees>
                  <t:Mailbox><t:Name>Alice</t:Name><t:EmailAddress>alice@test.com</t:EmailAddress></t:Mailbox>
                </t:RequiredAttendees>
              </t:CalendarItem>
            </t:CalendarItems>
          </m:RootFolder>
        </m:FindItemResponseMessage>
      </m:ResponseMessages>
    </m:FindItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: "test", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	p := NewEWSProvider(client)

	rng := TimeRange{
		Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 5, 2, 0, 0, 0, 0, time.UTC),
	}
	events, err := p.ListEvents(context.Background(), "", rng)
	if err != nil {
		t.Fatalf("ListEvents() error: %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("events = %d, want 1", len(events))
	}
	if events[0].Summary != "Meeting" {
		t.Fatalf("Summary = %q, want Meeting", events[0].Summary)
	}
	if events[0].ICSUID != "uid-1@test.com" {
		t.Fatalf("ICSUID = %q, want uid-1@test.com", events[0].ICSUID)
	}
	if events[0].Organizer != "org@test.com" {
		t.Fatalf("Organizer = %q, want org@test.com", events[0].Organizer)
	}
	if len(events[0].Attendees) != 1 || events[0].Attendees[0].Email != "alice@test.com" {
		t.Fatalf("Attendees = %+v, want alice@test.com", events[0].Attendees)
	}
}

func TestEWSProviderRespondToInviteSelectsCorrectElement(t *testing.T) {
	var soapAction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateItemResponse>
      <m:ResponseMessages>
        <m:CreateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: "test", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	p := NewEWSProvider(client)

	// Test accept
	err = p.RespondToInvite(context.Background(), "ev-1", providerdata.InviteResponse{Status: "accepted"})
	if err != nil {
		t.Fatalf("RespondToInvite(accepted) error: %v", err)
	}
	if !strings.Contains(soapAction, "CreateItem") {
		t.Fatalf("SOAPAction = %q, want CreateItem", soapAction)
	}

	// Test decline
	err = p.RespondToInvite(context.Background(), "ev-1", providerdata.InviteResponse{Status: "declined"})
	if err != nil {
		t.Fatalf("RespondToInvite(declined) error: %v", err)
	}

	// Test tentative
	err = p.RespondToInvite(context.Background(), "ev-1", providerdata.InviteResponse{Status: "tentative"})
	if err != nil {
		t.Fatalf("RespondToInvite(tentative) error: %v", err)
	}
}

func TestEWSProviderCloseIsNoop(t *testing.T) {
	p := NewEWSProvider(nil)
	if err := p.Close(); err != nil {
		t.Fatalf("Close() error: %v", err)
	}
}

func TestEventFromEWSMapsAllFields(t *testing.T) {
	item := ews.CalendarItem{
		ID:         "ev-1",
		Subject:    "All Day Event",
		Body:       "Description",
		Location:   "Here",
		Start:      time.Date(2026, 6, 1, 0, 0, 0, 0, time.UTC),
		End:        time.Date(2026, 6, 2, 0, 0, 0, 0, time.UTC),
		IsAllDay:   true,
		UID:        "uid-all-day@test.com",
		Organizer:  "boss@test.com",
		Status:     "confirmed",
		Recurrence: "FREQ=DAILY;COUNT=5",
	}
	ev := eventFromEWS(item, "cal-primary")
	if ev.ID != "ev-1" {
		t.Fatalf("ID = %q, want ev-1", ev.ID)
	}
	if ev.CalendarID != "cal-primary" {
		t.Fatalf("CalendarID = %q, want cal-primary", ev.CalendarID)
	}
	if ev.Summary != "All Day Event" {
		t.Fatalf("Summary = %q, want All Day Event", ev.Summary)
	}
	if ev.Description != "Description" {
		t.Fatalf("Description = %q, want Description", ev.Description)
	}
	if ev.AllDay != true {
		t.Fatalf("AllDay = %v, want true", ev.AllDay)
	}
	if ev.ICSUID != "uid-all-day@test.com" {
		t.Fatalf("ICSUID = %q, want uid-all-day@test.com", ev.ICSUID)
	}
	if ev.Organizer != "boss@test.com" {
		t.Fatalf("Organizer = %q, want boss@test.com", ev.Organizer)
	}
	if ev.Recurring != true {
		t.Fatalf("Recurring = %v, want true", ev.Recurring)
	}
}

func TestEventFromEWSSetsDefaultSummary(t *testing.T) {
	item := ews.CalendarItem{ID: "ev-empty"}
	ev := eventFromEWS(item, "cal")
	if ev.Summary != "(No title)" {
		t.Fatalf("Summary = %q, want (No title)", ev.Summary)
	}
}
