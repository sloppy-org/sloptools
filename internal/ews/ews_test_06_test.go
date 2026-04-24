package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const delegateSuccessResponse = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetDelegateResponse ResponseClass="Success" xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseCode>NoError</ResponseCode>
      <ResponseMessages>
        <DelegateUserResponseMessageType ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <DelegateUser xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
            <UserId>
              <PrimarySmtpAddress>jane@example.com</PrimarySmtpAddress>
              <DisplayName>Jane Doe</DisplayName>
            </UserId>
            <DelegatePermissions>
              <CalendarFolderPermissionLevel>Editor</CalendarFolderPermissionLevel>
              <TasksFolderPermissionLevel>Reviewer</TasksFolderPermissionLevel>
              <InboxFolderPermissionLevel>None</InboxFolderPermissionLevel>
              <ContactsFolderPermissionLevel>None</ContactsFolderPermissionLevel>
              <NotesFolderPermissionLevel>None</NotesFolderPermissionLevel>
              <JournalFolderPermissionLevel>None</JournalFolderPermissionLevel>
            </DelegatePermissions>
            <ReceiveCopiesOfMeetingMessages>true</ReceiveCopiesOfMeetingMessages>
            <ViewPrivateItems>false</ViewPrivateItems>
          </DelegateUser>
        </DelegateUserResponseMessageType>
        <DelegateUserResponseMessageType ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <DelegateUser xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
            <UserId>
              <PrimarySmtpAddress>bob@example.com</PrimarySmtpAddress>
              <DisplayName>Bob Smith</DisplayName>
            </UserId>
            <DelegatePermissions>
              <CalendarFolderPermissionLevel>Reviewer</CalendarFolderPermissionLevel>
            </DelegatePermissions>
            <ReceiveCopiesOfMeetingMessages>false</ReceiveCopiesOfMeetingMessages>
            <ViewPrivateItems>true</ViewPrivateItems>
          </DelegateUser>
        </DelegateUserResponseMessageType>
      </ResponseMessages>
      <DeliverMeetingRequests>DelegatesAndMe</DeliverMeetingRequests>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`

const delegateEmptyResponse = `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetDelegateResponse ResponseClass="Success" xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseCode>NoError</ResponseCode>
      <ResponseMessages/>
      <DeliverMeetingRequests>DelegatesAndMe</DeliverMeetingRequests>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`

func TestClientGetDelegateParsesDelegateUsers(t *testing.T) {
	var (
		body       string
		soapAction string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		soapAction = r.Header.Get("SOAPAction")
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, delegateSuccessResponse)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "delegate-1", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetDelegate(t.Context(), "albert@tugraz.at")
	if err != nil {
		t.Fatalf("GetDelegate() error: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("len(got) = %d, want 2", len(got))
	}
	if got[0].PrimarySmtpAddress != "jane@example.com" || got[0].DisplayName != "Jane Doe" {
		t.Fatalf("got[0] identity = %+v", got[0])
	}
	if got[0].Permissions.CalendarFolder != "Editor" {
		t.Fatalf("got[0].Permissions.CalendarFolder = %q, want Editor", got[0].Permissions.CalendarFolder)
	}
	if got[0].Permissions.TasksFolder != "Reviewer" {
		t.Fatalf("got[0].Permissions.TasksFolder = %q, want Reviewer", got[0].Permissions.TasksFolder)
	}
	if got[0].Permissions.InboxFolder != "None" {
		t.Fatalf("got[0].Permissions.InboxFolder = %q, want None", got[0].Permissions.InboxFolder)
	}
	if !got[0].ReceiveCopiesOfMR {
		t.Fatal("got[0].ReceiveCopiesOfMR = false, want true")
	}
	if got[0].ViewPrivateItems {
		t.Fatal("got[0].ViewPrivateItems = true, want false")
	}
	if got[1].PrimarySmtpAddress != "bob@example.com" {
		t.Fatalf("got[1].PrimarySmtpAddress = %q", got[1].PrimarySmtpAddress)
	}
	if !got[1].ViewPrivateItems {
		t.Fatal("got[1].ViewPrivateItems = false, want true")
	}
	if !strings.Contains(soapAction, "GetDelegate") {
		t.Fatalf("SOAPAction = %q, want GetDelegate", soapAction)
	}
	for _, snippet := range []string{
		`<m:GetDelegate IncludePermissions="true">`,
		`<m:Mailbox><t:EmailAddress>albert@tugraz.at</t:EmailAddress></m:Mailbox>`,
	} {
		if !strings.Contains(body, snippet) {
			t.Fatalf("request body missing %q:\n%s", snippet, body)
		}
	}
}

func TestClientGetDelegateReturnsEmptyListWhenNoDelegates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, delegateEmptyResponse)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "delegate-2", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetDelegate(t.Context(), "ada@example.com")
	if err != nil {
		t.Fatalf("GetDelegate() error: %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestClientGetDelegateSkipsFailedDelegateMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetDelegateResponse ResponseClass="Success" xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseCode>NoError</ResponseCode>
      <ResponseMessages>
        <DelegateUserResponseMessageType ResponseClass="Success">
          <ResponseCode>NoError</ResponseCode>
          <DelegateUser xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
            <UserId><PrimarySmtpAddress>ok@example.com</PrimarySmtpAddress><DisplayName>OK</DisplayName></UserId>
          </DelegateUser>
        </DelegateUserResponseMessageType>
        <DelegateUserResponseMessageType ResponseClass="Error">
          <ResponseCode>ErrorItemNotFound</ResponseCode>
          <DelegateUser xmlns="http://schemas.microsoft.com/exchange/services/2006/types">
            <UserId><PrimarySmtpAddress>broken@example.com</PrimarySmtpAddress></UserId>
          </DelegateUser>
        </DelegateUserResponseMessageType>
      </ResponseMessages>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "delegate-3", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	got, err := client.GetDelegate(t.Context(), "albert@tugraz.at")
	if err != nil {
		t.Fatalf("GetDelegate() error: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1 (error-response entries should be skipped)", len(got))
	}
	if got[0].PrimarySmtpAddress != "ok@example.com" {
		t.Fatalf("got[0].PrimarySmtpAddress = %q", got[0].PrimarySmtpAddress)
	}
}

func TestClientGetDelegateSurfacesTopLevelError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetDelegateResponse ResponseClass="Error" xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseCode>ErrorAccessDenied</ResponseCode>
      <MessageText>Access is denied.</MessageText>
      <ResponseMessages/>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "delegate-4", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	_, err = client.GetDelegate(t.Context(), "albert@tugraz.at")
	if err == nil {
		t.Fatal("GetDelegate() error = nil, want access-denied")
	}
	if !strings.Contains(err.Error(), "ErrorAccessDenied") {
		t.Fatalf("GetDelegate() error = %v, want ErrorAccessDenied", err)
	}
}

func TestClientGetDelegateRequiresMailbox(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "http://example.invalid", Username: "x", Password: "y"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	if _, err := client.GetDelegate(t.Context(), "  "); err == nil {
		t.Fatal("GetDelegate() error = nil, want missing-mailbox error")
	}
}
