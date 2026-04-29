package mailboxsettings

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/ews"
)

const delegateResponseXML = `<?xml version="1.0" encoding="utf-8"?>
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
            </DelegatePermissions>
            <ReceiveCopiesOfMeetingMessages>true</ReceiveCopiesOfMeetingMessages>
            <ViewPrivateItems>false</ViewPrivateItems>
          </DelegateUser>
        </DelegateUserResponseMessageType>
      </ResponseMessages>
      <DeliverMeetingRequests>DelegatesAndMe</DeliverMeetingRequests>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`

func TestEWSProviderListDelegatesMapsPermissions(t *testing.T) {
	var requestBody string
	provider, cleanup := newFakeEWSProvider(t, "ada@example.com", func(w http.ResponseWriter, r *http.Request) {
		data, _ := io.ReadAll(r.Body)
		requestBody = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, delegateResponseXML)
	})
	defer cleanup()

	got, err := provider.ListDelegates(t.Context())
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("len(got) = %d, want 1", len(got))
	}
	if got[0].Email != "jane@example.com" {
		t.Fatalf("got[0].Email = %q", got[0].Email)
	}
	if got[0].Name != "Jane Doe" {
		t.Fatalf("got[0].Name = %q", got[0].Name)
	}
	wantPerms := []string{"calendar:Editor", "tasks:Reviewer"}
	if len(got[0].Permissions) != len(wantPerms) {
		t.Fatalf("got[0].Permissions = %v, want %v", got[0].Permissions, wantPerms)
	}
	for i := range wantPerms {
		if got[0].Permissions[i] != wantPerms[i] {
			t.Fatalf("got[0].Permissions[%d] = %q, want %q", i, got[0].Permissions[i], wantPerms[i])
		}
	}
	if !strings.Contains(requestBody, `<t:EmailAddress>ada@example.com</t:EmailAddress>`) {
		t.Fatalf("request body missing mailbox address:\n%s", requestBody)
	}
}

func TestEWSProviderListDelegatesReturnsEmptySliceForEmptyResponseMessages(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/">
  <soap:Body>
    <GetDelegateResponse ResponseClass="Success" xmlns="http://schemas.microsoft.com/exchange/services/2006/messages">
      <ResponseCode>NoError</ResponseCode>
      <ResponseMessages/>
    </GetDelegateResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: "empty", Password: "secret"})
	if err != nil {
		t.Fatalf("ews.NewClient: %v", err)
	}
	defer client.Close()
	provider := NewEWSProvider(client, "empty@example.com")

	got, err := provider.ListDelegates(t.Context())
	if err != nil {
		t.Fatalf("ListDelegates: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want non-nil empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
}

func TestEWSProviderListSharedMailboxesReturnsEmptySlice(t *testing.T) {
	provider := NewEWSProvider(nil, "any@example.com")
	got, err := provider.ListSharedMailboxes(t.Context())
	if err != nil {
		t.Fatalf("ListSharedMailboxes: %v", err)
	}
	if got == nil {
		t.Fatal("got = nil, want empty slice")
	}
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0 (EWS cannot enumerate shared mailboxes yet)", len(got))
	}
}

func TestEWSProviderListDelegatesRequiresConfiguration(t *testing.T) {
	provider := NewEWSProvider(nil, "x@example.com")
	if _, err := provider.ListDelegates(t.Context()); err == nil {
		t.Fatal("ListDelegates() error = nil, want client-not-configured")
	}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	defer server.Close()
	client, err := ews.NewClient(ews.Config{Endpoint: server.URL, Username: "u", Password: "p"})
	if err != nil {
		t.Fatalf("ews.NewClient: %v", err)
	}
	defer client.Close()
	noMailbox := NewEWSProvider(client, "")
	if _, err := noMailbox.ListDelegates(t.Context()); err == nil {
		t.Fatal("ListDelegates() error = nil, want mailbox-required")
	}
}
