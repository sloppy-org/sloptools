package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientCreateCalendarItemBuildsSOAPAndParsesItemID(t *testing.T) {
	var (
		body       string
		soapAction string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
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
          <m:Items>
            <t:CalendarItem>
              <t:ItemId Id="cal-1" ChangeKey="ck-1" />
            </t:CalendarItem>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-cal-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	start := time.Date(2026, 5, 1, 10, 0, 0, 0, time.UTC)
	end := time.Date(2026, 5, 1, 11, 0, 0, 0, time.UTC)
	itemID, changeKey, err := client.CreateCalendarItem(t.Context(), "", CalendarItemInput{
		Subject:  "Team Standup",
		Body:     "Daily sync",
		Location: "Room A",
		Start:    start,
		End:      end,
		UID:      "abc123@example.com",
		RequiredAttendees: []EventAttendee{
			{Email: "alice@example.com", Name: "Alice"},
		},
		SendInvitations: "SendOnlyToAll",
	}, "SendOnlyToAll")
	if err != nil {
		t.Fatalf("CreateCalendarItem() error: %v", err)
	}
	if itemID != "cal-1" || changeKey != "ck-1" {
		t.Fatalf("itemID=%q changeKey=%q, want cal-1/ck-1", itemID, changeKey)
	}
	if !strings.Contains(soapAction, "CreateItem") {
		t.Fatalf("SOAPAction = %q, want CreateItem", soapAction)
	}
	for _, snippet := range []string{
		`<m:CreateItem MessageDisposition="SaveOnly"`,
		`<t:DistinguishedFolderId Id="calendar" />`,
		`<t:CalendarItem>`,
		`<t:Subject>Team Standup</t:Subject>`,
		`<t:Location>Room A</t:Location>`,
		`<t:Start>2026-05-01T10:00:00Z</t:Start>`,
		`<t:End>2026-05-01T11:00:00Z</t:End>`,
		`<t:UID>abc123@example.com</t:UID>`,
		`<t:RequiredAttendees>`,
		`<t:EmailAddress>alice@example.com</t:EmailAddress>`,
		`SendOnlyToAll`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("body missing %q", snippet)
		}
	}
}

func TestClientCreateCalendarItemAllDayBuildsCorrectXML(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
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
          <m:Items>
            <t:CalendarItem>
              <t:ItemId Id="cal-2" ChangeKey="ck-2" />
            </t:CalendarItem>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-cal-2", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	start := time.Date(2026, 6, 15, 0, 0, 0, 0, time.UTC)
	end := time.Date(2026, 6, 16, 0, 0, 0, 0, time.UTC)
	_, _, err = client.CreateCalendarItem(t.Context(), "", CalendarItemInput{
		Subject:  "Company Holiday",
		Start:    start,
		End:      end,
		IsAllDay: true,
	}, "SendToNone")
	if err != nil {
		t.Fatalf("CreateCalendarItem() error: %v", err)
	}
	if !strings.Contains(body, `<t:IsAllDayEvent>true</t:IsAllDayEvent>`) {
		t.Fatalf("body missing IsAllDayEvent=true: %s", body)
	}
	if !strings.Contains(body, `<t:Start>2026-06-15</t:Start>`) {
		t.Fatalf("body missing all-day start: %s", body)
	}
}

func TestClientGetCalendarItemParsesAttendeesAndUID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:GetItemResponse>
      <m:ResponseMessages>
        <m:GetItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:CalendarItem>
              <t:ItemId Id="cal-get-1" ChangeKey="ck-get-1" />
              <t:Subject>Quarterly Review</t:Subject>
              <t:Body BodyType="HTML">Agenda here</t:Body>
              <t:Location>Boardroom</t:Location>
              <t:Start>2026-07-01T14:00:00Z</t:Start>
              <t:End>2026-07-01T15:30:00Z</t:End>
              <t:UID>qr-2026@company.com</t:UID>
              <t:Organizer><t:Mailbox><t:Name>Boss</t:Name><t:EmailAddress>boss@company.com</t:EmailAddress></t:Mailbox></t:Organizer>
              <t:RequiredAttendees>
                <t:Mailbox><t:Name>Alice</t:Name><t:EmailAddress>alice@company.com</t:EmailAddress></t:Mailbox>
              </t:RequiredAttendees>
              <t:OptionalAttendees>
                <t:Mailbox><t:Name>Bob</t:Name><t:EmailAddress>bob@company.com</t:EmailAddress></t:Mailbox>
              </t:OptionalAttendees>
            </t:CalendarItem>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-get-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	item, err := client.GetCalendarItem(t.Context(), "cal-get-1")
	if err != nil {
		t.Fatalf("GetCalendarItem() error: %v", err)
	}
	if item.ID != "cal-get-1" {
		t.Fatalf("ID = %q, want cal-get-1", item.ID)
	}
	if item.Subject != "Quarterly Review" {
		t.Fatalf("Subject = %q, want Quarterly Review", item.Subject)
	}
	if item.UID != "qr-2026@company.com" {
		t.Fatalf("UID = %q, want qr-2026@company.com", item.UID)
	}
	if item.Organizer != "boss@company.com" {
		t.Fatalf("Organizer = %q, want boss@company.com", item.Organizer)
	}
	if len(item.RequiredAttendees) != 1 || item.RequiredAttendees[0].Email != "alice@company.com" {
		t.Fatalf("RequiredAttendees = %+v, want alice@company.com", item.RequiredAttendees)
	}
	if len(item.OptionalAttendees) != 1 || item.OptionalAttendees[0].Email != "bob@company.com" {
		t.Fatalf("OptionalAttendees = %+v, want bob@company.com", item.OptionalAttendees)
	}
}

func TestClientDeleteCalendarItemSendsHardDelete(t *testing.T) {
	var soapAction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:DeleteItemResponse>
      <m:ResponseMessages>
        <m:DeleteItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:DeleteItemResponseMessage>
      </m:ResponseMessages>
    </m:DeleteItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-del-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.DeleteCalendarItem(t.Context(), "cal-del-1", "SendToNone")
	if err != nil {
		t.Fatalf("DeleteCalendarItem() error: %v", err)
	}
	if !strings.Contains(soapAction, "DeleteItem") {
		t.Fatalf("SOAPAction = %q, want DeleteItem", soapAction)
	}
}

func TestClientCreateMeetingResponseSendsAcceptItem(t *testing.T) {
	var (
		body       string
		soapAction string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		raw, _ := io.ReadAll(r.Body)
		r.Body.Close()
		body = string(raw)
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
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-meet-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.CreateMeetingResponse(t.Context(), "cal-meet-1", "ck-meet-1", MeetingAccept, "Thanks, I'll be there")
	if err != nil {
		t.Fatalf("CreateMeetingResponse() error: %v", err)
	}
	if !strings.Contains(soapAction, "CreateItem") {
		t.Fatalf("SOAPAction = %q, want CreateItem", soapAction)
	}
	for _, snippet := range []string{
		`<t:AcceptItem>`,
		`<ReferenceItemId Id="cal-meet-1" ChangeKey="ck-meet-1" />`,
		`<t:Body BodyType="HTML">Thanks, I&#39;ll be there</t:Body>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("body missing %q", snippet)
		}
	}
}

func TestClientListCalendarItemsUsesCalendarView(t *testing.T) {
	var soapAction string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
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
                <t:ItemId Id="cal-list-1" ChangeKey="ck-list-1" />
                <t:Subject>Found Event</t:Subject>
                <t:Start>2026-05-01T09:00:00Z</t:Start>
                <t:End>2026-05-01T10:00:00Z</t:End>
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
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ews-list-1", Password: "secret", BatchSize: 50})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	rng := TimeRange{
		Start: time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC),
		End:   time.Date(2026, 5, 31, 0, 0, 0, 0, time.UTC),
	}
	items, err := client.ListCalendarItems(t.Context(), "", rng)
	if err != nil {
		t.Fatalf("ListCalendarItems() error: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("items = %d, want 1", len(items))
	}
	if items[0].Subject != "Found Event" {
		t.Fatalf("Subject = %q, want Found Event", items[0].Subject)
	}
	if !strings.Contains(soapAction, "FindItem") {
		t.Fatalf("SOAPAction = %q, want FindItem", soapAction)
	}
}
