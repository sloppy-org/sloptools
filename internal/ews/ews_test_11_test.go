package ews

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestClientGetTaskItemBuildsSOAPAndParsesTask(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
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
            <t:Task>
              <t:ItemId Id="task-1" ChangeKey="ck-1" />
              <t:ParentFolderId Id="tasks" ChangeKey="fold-1" />
              <t:Subject>Buy groceries</t:Subject>
              <t:Body BodyType="Text">Milk and eggs</t:Body>
              <t:Status>NotStarted</t:Status>
              <t:DueDate>2026-06-15T00:00:00Z</t:DueDate>
            </t:Task>
          </m:Items>
        </m:GetItemResponseMessage>
      </m:ResponseMessages>
    </m:GetItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	task, err := client.GetTaskItem(t.Context(), "task-1")
	if err != nil {
		t.Fatalf("GetTaskItem() error: %v", err)
	}
	if task.ID != "task-1" {
		t.Fatalf("task.ID = %q, want task-1", task.ID)
	}
	if task.Subject != "Buy groceries" {
		t.Fatalf("task.Subject = %q, want Buy groceries", task.Subject)
	}
	if task.Status != "NotStarted" {
		t.Fatalf("task.Status = %q, want NotStarted", task.Status)
	}
	if task.DueDate == nil {
		t.Fatalf("task.DueDate is nil, want non-nil")
	}
	if !strings.Contains(body, `<m:GetItem>`) {
		t.Fatalf("request body missing GetItem:\n%s", body)
	}
}

func TestClientGetTaskItemRejectsEmptyID(t *testing.T) {
	client, err := NewClient(Config{Endpoint: "http://example.com", Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()
	_, err = client.GetTaskItem(t.Context(), "")
	if err == nil {
		t.Fatalf("GetTaskItem(\"\") returned nil error")
	}
}

func TestClientCreateTaskItemBuildsSOAPAndParsesItemID(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
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
            <t:Task>
              <t:ItemId Id="task-new" ChangeKey="ck-new" />
              <t:Subject>Test task</t:Subject>
              <t:Status>NotStarted</t:Status>
            </t:Task>
          </m:Items>
        </m:CreateItemResponseMessage>
      </m:ResponseMessages>
    </m:CreateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	itemID, changeKey, err := client.CreateTaskItem(t.Context(), "tasks", TaskInput{
		Subject: "Test task",
		Body:    "A test task body",
		Status:  "NotStarted",
	})
	if err != nil {
		t.Fatalf("CreateTaskItem() error: %v", err)
	}
	if itemID != "task-new" || changeKey != "ck-new" {
		t.Fatalf("itemID=%q changeKey=%q, want task-new/ck-new", itemID, changeKey)
	}
	if !strings.Contains(body, `<m:CreateItem`) {
		t.Fatalf("request body missing CreateItem:\n%s", body)
	}
	if !strings.Contains(body, `<t:Task>`) {
		t.Fatalf("request body missing Task element:\n%s", body)
	}
	if !strings.Contains(body, `<t:Subject>Test task</t:Subject>`) {
		t.Fatalf("request body missing Subject:\n%s", body)
	}
}

func TestClientUpdateTaskItemBuildsSOAP(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:UpdateItemResponse>
      <m:ResponseMessages>
        <m:UpdateItemResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Items>
            <t:Task>
              <t:ItemId Id="task-1" ChangeKey="ck-updated" />
              <t:Subject>Updated task</t:Subject>
              <t:Status>Completed</t:Status>
            </t:Task>
          </m:Items>
        </m:UpdateItemResponseMessage>
      </m:ResponseMessages>
    </m:UpdateItemResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	subject := "Updated task"
	status := "Completed"
	updates := TaskUpdate{
		Subject: &subject,
		Status:  &status,
	}
	newKey, err := client.UpdateTaskItem(t.Context(), "task-1", "ck-1", updates)
	if err != nil {
		t.Fatalf("UpdateTaskItem() error: %v", err)
	}
	if newKey != "ck-updated" {
		t.Fatalf("newKey = %q, want ck-updated", newKey)
	}
	if !strings.Contains(body, `<m:UpdateItem`) {
		t.Fatalf("request body missing UpdateItem:\n%s", body)
	}
	if !strings.Contains(body, `<t:Subject>Updated task</t:Subject>`) {
		t.Fatalf("request body missing Subject:\n%s", body)
	}
}

func TestClientDeleteTaskItemBuildsSOAP(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
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
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.DeleteTaskItem(t.Context(), "task-1")
	if err != nil {
		t.Fatalf("DeleteTaskItem() error: %v", err)
	}
	if !strings.Contains(body, `<m:DeleteItem`) {
		t.Fatalf("request body missing DeleteItem:\n%s", body)
	}
	if !strings.Contains(body, `DeleteType="MoveToDeletedItems"`) {
		t.Fatalf("request body missing MoveToDeletedItems:\n%s", body)
	}
}

func TestClientCreateTasksFolderBuildsSOAPAndParsesFolderID(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:CreateFolderResponse>
      <m:ResponseMessages>
        <m:CreateFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:Folders>
            <t:TasksFolder>
              <t:FolderId Id="folder-1" ChangeKey="ck-fold-1" />
              <t:DisplayName>Shopping</t:DisplayName>
            </t:TasksFolder>
          </m:Folders>
        </m:CreateFolderResponseMessage>
      </m:ResponseMessages>
    </m:CreateFolderResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	folderID, changeKey, err := client.CreateTasksFolder(t.Context(), TasksFolderInput{
		ParentFolderID: "tasks",
		DisplayName:    "Shopping",
	})
	if err != nil {
		t.Fatalf("CreateTasksFolder() error: %v", err)
	}
	if folderID != "folder-1" || changeKey != "ck-fold-1" {
		t.Fatalf("folderID=%q changeKey=%q, want folder-1/ck-fold-1", folderID, changeKey)
	}
	if !strings.Contains(body, `<m:CreateFolder`) {
		t.Fatalf("request body missing CreateFolder:\n%s", body)
	}
	if !strings.Contains(body, `<t:TasksFolder>`) {
		t.Fatalf("request body missing TasksFolder:\n%s", body)
	}
}

func TestClientDeleteFolderBuildsSOAP(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:DeleteFolderResponse>
      <m:ResponseMessages>
        <m:DeleteFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
        </m:DeleteFolderResponseMessage>
      </m:ResponseMessages>
    </m:DeleteFolderResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	err = client.DeleteFolder(t.Context(), "folder-1")
	if err != nil {
		t.Fatalf("DeleteFolder() error: %v", err)
	}
	if !strings.Contains(body, `<m:DeleteFolder`) {
		t.Fatalf("request body missing DeleteFolder:\n%s", body)
	}
}

func TestClientListTasksFoldersBuildsSOAPAndParsesFolders(t *testing.T) {
	var body string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		data, _ := io.ReadAll(r.Body)
		body = string(data)
		w.Header().Set("Content-Type", "text/xml; charset=utf-8")
		_, _ = io.WriteString(w, `<?xml version="1.0" encoding="utf-8"?>
<soap:Envelope xmlns:soap="http://schemas.xmlsoap.org/soap/envelope/"
               xmlns:m="http://schemas.microsoft.com/exchange/services/2006/messages"
               xmlns:t="http://schemas.microsoft.com/exchange/services/2006/types">
  <soap:Body>
    <m:FindFolderResponse>
      <m:ResponseMessages>
        <m:FindFolderResponseMessage ResponseClass="Success">
          <m:ResponseCode>NoError</m:ResponseCode>
          <m:RootFolder>
            <m:TotalItemsInView>2</m:TotalItemsInView>
            <m:Folders>
              <t:TasksFolder>
                <t:FolderId Id="tasks" ChangeKey="ck-primary" />
                <t:DisplayName>Tasks</t:DisplayName>
                <t:TotalCount>5</t:TotalCount>
              </t:TasksFolder>
              <t:TasksFolder>
                <t:FolderId Id="folder-sub" ChangeKey="ck-sub" />
                <t:DisplayName>Shopping</t:DisplayName>
                <t:TotalCount>3</t:TotalCount>
              </t:TasksFolder>
            </m:Folders>
          </m:RootFolder>
        </m:FindFolderResponseMessage>
      </m:ResponseMessages>
    </m:FindFolderResponse>
  </soap:Body>
</soap:Envelope>`)
	}))
	defer server.Close()
	client, err := NewClient(Config{Endpoint: server.URL, Username: "ert", Password: "secret"})
	if err != nil {
		t.Fatalf("NewClient() error: %v", err)
	}
	defer client.Close()

	folders, err := client.ListTasksFolders(t.Context())
	if err != nil {
		t.Fatalf("ListTasksFolders() error: %v", err)
	}
	if len(folders) != 2 {
		t.Fatalf("len(folders) = %d, want 2", len(folders))
	}
	if folders[0].ID != "tasks" {
		t.Fatalf("folders[0].ID = %q, want tasks", folders[0].ID)
	}
	if folders[1].ID != "folder-sub" {
		t.Fatalf("folders[1].ID = %q, want folder-sub", folders[1].ID)
	}
	if !strings.Contains(body, `<m:FindFolder`) {
		t.Fatalf("request body missing FindFolder:\n%s", body)
	}
}

func TestTaskUpdateXMLClearsFieldsWhenEmpty(t *testing.T) {
	updates := TaskUpdate{
		Subject: strPtr(""),
		Body:    strPtr(""),
	}
	xml := taskUpdateXML(updates)
	if !strings.Contains(xml, `<t:DeleteItemField><t:FieldURI FieldURI="task:Subject" />`) {
		t.Fatalf("expected DeleteItemField for Subject:\n%s", xml)
	}
	if !strings.Contains(xml, `<t:DeleteItemField><t:FieldURI FieldURI="task:Body" />`) {
		t.Fatalf("expected DeleteItemField for Body:\n%s", xml)
	}
}

func TestTaskUpdateXMLSetsFieldsWhenProvided(t *testing.T) {
	now := time.Now().UTC()
	updates := TaskUpdate{
		Subject:    strPtr("New subject"),
		StartDate:  &now,
		DueDate:    &now,
		Status:     strPtr("Completed"),
		Importance: strPtr("High"),
		IsComplete: boolPtr(true),
	}
	xml := taskUpdateXML(updates)
	for _, want := range []string{
		`<t:SetItemField><t:FieldURI FieldURI="task:Subject"><t:Task><t:Subject>New subject</t:Subject>`,
		`<t:SetItemField><t:FieldURI FieldURI="task:StartDate">`,
		`<t:SetItemField><t:FieldURI FieldURI="task:DueDate">`,
		`<t:SetItemField><t:FieldURI FieldURI="task:Status"><t:Task><t:Status>Completed</t:Status>`,
		`<t:SetItemField><t:FieldURI FieldURI="task:Importance"><t:Task><t:Importance>High</t:Importance>`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("expected %q in:\n%s", want, xml)
		}
	}
}
