package ews

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// TaskInput carries the fields needed to create a new EWS Task item.
type TaskInput struct {
	Subject       string
	Body          string
	BodyType      string
	StartDate     *time.Time
	DueDate       *time.Time
	Status        string
	Importance    string
	ReminderSet   bool
	ReminderDueBy *time.Time
}

// TaskUpdate carries the optional fields for updating an existing EWS Task item.
// A nil pointer means "leave the field unchanged"; an empty string means "clear it".
type TaskUpdate struct {
	Subject       *string
	Body          *string
	BodyType      *string
	StartDate     *time.Time
	DueDate       *time.Time
	Status        *string
	Importance    *string
	IsComplete    *bool
	CompleteDate  *time.Time
	ReminderSet   *bool
	ReminderDueBy *time.Time
}

// GetTaskItem fetches a single task item by its ItemId.
func (c *Client) GetTaskItem(ctx context.Context, itemID string) (Task, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Task{}, fmt.Errorf("ews GetTaskItem: item id is required")
	}
	var resp getTaskItemEnvelope
	if err := c.call(ctx, "GetItem", getItemBody([]string{itemID}, true), &resp); err != nil {
		return Task{}, err
	}
	items := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Tasks
	if len(items) == 0 {
		return Task{}, fmt.Errorf("ews GetTaskItem: task %q not found", itemID)
	}
	return items[0].toTask(), nil
}

// CreateTaskItem inserts a new task into the given folder and returns the
// server-assigned ItemId and ChangeKey.
func (c *Client) CreateTaskItem(ctx context.Context, parentFolderID string, item TaskInput) (itemID, changeKey string, err error) {
	folder := folderIDOrDistinguished(parentFolderID, "tasks")
	body := `<m:CreateItem MessageDisposition="SaveOnly">` +
		`<m:SavedItemFolderId>` + folderIDXML(folder) +
		`</m:SavedItemFolderId><m:Items>` + taskItemCreateXML(item) +
		`</m:Items></m:CreateItem>`
	var resp createTaskItemEnvelope
	if err := c.call(ctx, "CreateItem", body, &resp); err != nil {
		return "", "", err
	}
	values := resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Tasks
	if len(values) == 0 {
		return "", "", fmt.Errorf("ews CreateTaskItem returned no items")
	}
	return strings.TrimSpace(values[0].ItemID.ID), strings.TrimSpace(values[0].ItemID.ChangeKey), nil
}

// UpdateTaskItem applies field-level updates to an existing task. It returns
// the new ChangeKey. Nil fields in updates are left untouched; empty-string
// pointers clear the field via DeleteItemField.
func (c *Client) UpdateTaskItem(ctx context.Context, itemID, changeKey string, updates TaskUpdate) (newChangeKey string, err error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return "", fmt.Errorf("ews UpdateTaskItem: item id is required")
	}
	changes := taskUpdateXML(updates)
	if strings.TrimSpace(changes) == "" {
		return strings.TrimSpace(changeKey), nil
	}
	var b strings.Builder
	b.WriteString(`<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AutoResolve">`)
	b.WriteString(`<m:ItemChanges><t:ItemChange><t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(itemID))
	if ck := strings.TrimSpace(changeKey); ck != "" {
		b.WriteString(`" ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
	}
	b.WriteString(`" /><t:Updates>`)
	b.WriteString(changes)
	b.WriteString(`</t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`)
	var resp updateTaskItemEnvelope
	if err := c.call(ctx, "UpdateItem", b.String(), &resp); err != nil {
		return "", err
	}
	newKey := strings.TrimSpace(resp.Body.UpdateItemResponse.ResponseMessages.Message.Items.Tasks[0].ItemID.ChangeKey)
	if newKey == "" {
		newKey = strings.TrimSpace(changeKey)
	}
	return newKey, nil
}

// DeleteTaskItem permanently removes a task via DeleteItem with MoveToDeletedItems.
func (c *Client) DeleteTaskItem(ctx context.Context, itemID string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("ews DeleteTaskItem: item id is required")
	}
	body := `<m:DeleteItem DeleteType="MoveToDeletedItems">` +
		`<m:ItemIds><t:ItemId Id="` + xmlEscapeAttr(itemID) + `" /></m:ItemIds></m:DeleteItem>`
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

// TasksFolderInput carries the fields needed to create a new tasks folder.
type TasksFolderInput struct {
	ParentFolderID string
	DisplayName    string
}

// CreateTasksFolder creates a new tasks subfolder under the given parent and
// returns the folder ID and change key.
func (c *Client) CreateTasksFolder(ctx context.Context, input TasksFolderInput) (folderID, changeKey string, err error) {
	parent := folderIDOrDistinguished(input.ParentFolderID, "tasks")
	body := `<m:CreateFolder>` +
		`<m:ParentFolderId>` + folderIDXML(parent) +
		`</m:ParentFolderId><m:Folders><t:TasksFolder><t:DisplayName>` +
		xmlEscapeText(strings.TrimSpace(input.DisplayName)) +
		`</t:DisplayName></t:TasksFolder></m:Folders></m:CreateFolder>`
	var resp createFolderEnvelope
	if err := c.call(ctx, "CreateFolder", body, &resp); err != nil {
		return "", "", err
	}
	folders := resp.Body.CreateFolderResponse.ResponseMessages.Message.Folders.TasksFolders
	if len(folders) == 0 {
		return "", "", fmt.Errorf("ews CreateTasksFolder returned no folders")
	}
	return strings.TrimSpace(folders[0].FolderID.ID), strings.TrimSpace(folders[0].FolderID.ChangeKey), nil
} // CreateTasksFolder creates a new tasks subfolder under the given parent. The
// parentFolderID may be empty to default to the distinguished "tasks" folder.

// DeleteFolder removes a folder via DeleteFolder with MoveToDeletedItems.
func (c *Client) DeleteFolder(ctx context.Context, folderID string) error {
	folderID = strings.TrimSpace(folderID)
	if folderID == "" {
		return fmt.Errorf("ews DeleteFolder: folder id is required")
	}
	body := `<m:DeleteFolder DeleteType="MoveToDeletedItems">` +
		`<m:FolderIds><t:FolderId Id="` + xmlEscapeAttr(folderID) + `" /></m:FolderIds></m:DeleteFolder>`
	var resp deleteFolderEnvelope
	return c.call(ctx, "DeleteFolder", body, &resp)
}

// ListTasksFolders enumerates all tasks folders under the distinguished tasks
// folder, including the primary tasks folder itself and any subfolders.
func (c *Client) ListTasksFolders(ctx context.Context) ([]Folder, error) {
	var resp findTasksFolderEnvelope
	body := `<m:FindFolder Traversal="Deep">` +
		`<m:FolderShape><t:BaseShape>IdOnly</t:BaseShape></m:FolderShape>` +
		`<m:ParentFolderIds><t:DistinguishedFolderId Id="tasks" /></m:ParentFolderIds>` +
		`</m:FindFolder>`
	if err := c.call(ctx, "FindFolder", body, &resp); err != nil {
		return nil, err
	}
	return resp.Body.FindFolderResponse.ResponseMessages.Message.Root.Folders.toFolders(), nil
}

// -- XML helpers --

func taskItemCreateXML(item TaskInput) string {
	var b strings.Builder
	b.WriteString(`<t:Task>`)
	writeOptionalElement(&b, "Subject", item.Subject)
	if clean := strings.TrimSpace(item.Body); clean != "" {
		bodyType := "Text"
		if item.BodyType != "" {
			bodyType = item.BodyType
		}
		b.WriteString(`<t:Body BodyType="`)
		b.WriteString(bodyType)
		b.WriteString(`">`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Body>`)
	}
	if item.StartDate != nil && !item.StartDate.IsZero() {
		b.WriteString(`<t:StartDate>`)
		b.WriteString(item.StartDate.UTC().Format(time.RFC3339))
		b.WriteString(`</t:StartDate>`)
	}
	if item.DueDate != nil && !item.DueDate.IsZero() {
		b.WriteString(`<t:DueDate>`)
		b.WriteString(item.DueDate.UTC().Format(time.RFC3339))
		b.WriteString(`</t:DueDate>`)
	}
	if clean := strings.TrimSpace(item.Status); clean != "" {
		b.WriteString(`<t:Status>`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Status>`)
	}
	if clean := strings.TrimSpace(item.Importance); clean != "" {
		b.WriteString(`<t:Importance>`)
		b.WriteString(xmlEscapeText(clean))
		b.WriteString(`</t:Importance>`)
	}
	if item.ReminderSet {
		b.WriteString(`<t:ReminderIsSet>true</t:ReminderIsSet>`)
		if item.ReminderDueBy != nil && !item.ReminderDueBy.IsZero() {
			b.WriteString(`<t:ReminderDueBy>`)
			b.WriteString(item.ReminderDueBy.UTC().Format(time.RFC3339))
			b.WriteString(`</t:ReminderDueBy>`)
		}
	}
	b.WriteString(`</t:Task>`)
	return b.String()
}

func taskUpdateXML(updates TaskUpdate) string {
	var b strings.Builder
	if updates.Subject != nil {
		writeTaskSetItemField(&b, "Subject", "task:Subject", derefOrEmpty(*updates.Subject))
	}
	if updates.Body != nil {
		clean := strings.TrimSpace(*updates.Body)
		if clean == "" {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="task:Body" /></t:DeleteItemField>`)
		} else {
			bodyType := "Text"
			if updates.BodyType != nil && *updates.BodyType != "" {
				bodyType = *updates.BodyType
			}
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:Body"><t:Task><t:Body BodyType="`)
			b.WriteString(bodyType)
			b.WriteString(`">`)
			b.WriteString(xmlEscapeText(clean))
			b.WriteString(`</t:Body></t:Task></t:FieldURI></t:SetItemField>`)
		}
	}
	if updates.StartDate != nil {
		if updates.StartDate.IsZero() {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="task:StartDate" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:StartDate"><t:Task><t:StartDate>`)
			b.WriteString(updates.StartDate.UTC().Format(time.RFC3339))
			b.WriteString(`</t:StartDate></t:Task></t:FieldURI></t:SetItemField>`)
		}
	}
	if updates.DueDate != nil {
		if updates.DueDate.IsZero() {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="task:DueDate" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:DueDate"><t:Task><t:DueDate>`)
			b.WriteString(updates.DueDate.UTC().Format(time.RFC3339))
			b.WriteString(`</t:DueDate></t:Task></t:FieldURI></t:SetItemField>`)
		}
	}
	if updates.Status != nil {
		writeTaskSetItemField(&b, "Status", "task:Status", derefOrEmpty(*updates.Status))
	}
	if updates.Importance != nil {
		writeTaskSetItemField(&b, "Importance", "task:Importance", derefOrEmpty(*updates.Importance))
	}
	if updates.IsComplete != nil {
		val := "false"
		if *updates.IsComplete {
			val = "true"
		}
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:PercentComplete"><t:Task><t:PercentComplete>`)
		b.WriteString(val)
		b.WriteString(`</t:PercentComplete></t:Task></t:FieldURI></t:SetItemField>`)
	}
	if updates.CompleteDate != nil {
		if updates.CompleteDate.IsZero() {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="task:DateCompleted" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:DateCompleted"><t:Task><t:DateCompleted>`)
			b.WriteString(updates.CompleteDate.UTC().Format(time.RFC3339))
			b.WriteString(`</t:DateCompleted></t:Task></t:FieldURI></t:SetItemField>`)
		}
	}
	if updates.ReminderSet != nil {
		val := "false"
		if *updates.ReminderSet {
			val = "true"
		}
		b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:ReminderIsSet"><t:Task><t:ReminderIsSet>`)
		b.WriteString(val)
		b.WriteString(`</t:ReminderIsSet></t:Task></t:FieldURI></t:SetItemField>`)
	}
	if updates.ReminderDueBy != nil {
		if updates.ReminderDueBy.IsZero() {
			b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="task:ReminderDueBy" /></t:DeleteItemField>`)
		} else {
			b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="task:ReminderDueBy"><t:Task><t:ReminderDueBy>`)
			b.WriteString(updates.ReminderDueBy.UTC().Format(time.RFC3339))
			b.WriteString(`</t:ReminderDueBy></t:Task></t:FieldURI></t:SetItemField>`)
		}
	}
	return b.String()
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// writeTaskSetItemField writes a SetItemField or DeleteItemField for a Task
// item. Empty values are cleared via DeleteItemField.
func writeTaskSetItemField(b *strings.Builder, name, fieldURI, value string) {
	clean := strings.TrimSpace(value)
	if clean == "" {
		b.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="`)
		b.WriteString(fieldURI)
		b.WriteString(`" /></t:DeleteItemField>`)
		return
	}
	b.WriteString(`<t:SetItemField><t:FieldURI FieldURI="`)
	b.WriteString(fieldURI)
	b.WriteString(`"><t:Task><t:`)
	b.WriteString(name)
	b.WriteString(`>`)
	b.WriteString(xmlEscapeText(clean))
	b.WriteString(`</t:`)
	b.WriteString(name)
	b.WriteString(`></t:Task></t:FieldURI></t:SetItemField>`)
}

func derefOrEmpty(s string) string {
	clean := strings.TrimSpace(s)
	if clean == "" {
		return clean
	}
	return clean
}

// -- XML envelope types --

type getTaskItemEnvelope struct {
	Body struct {
		GetItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Tasks []itemXML `xml:"Task"`
					} `xml:"Items"`
				} `xml:"GetItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"GetItemResponse"`
	} `xml:"Body"`
}

type createTaskItemEnvelope struct {
	Body struct {
		CreateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Tasks []itemXML `xml:"Task"`
					} `xml:"Items"`
				} `xml:"CreateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateItemResponse"`
	} `xml:"Body"`
}

type updateTaskItemEnvelope struct {
	Body struct {
		UpdateItemResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Items        struct {
						Tasks []itemXML `xml:"Task"`
					} `xml:"Items"`
				} `xml:"UpdateItemResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"UpdateItemResponse"`
	} `xml:"Body"`
}

type createFolderEnvelope struct {
	Body struct {
		CreateFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
					Folders      struct {
						TasksFolders []folderXML `xml:"TasksFolder"`
					} `xml:"Folders"`
				} `xml:"CreateFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"CreateFolderResponse"`
	} `xml:"Body"`
}

type deleteFolderEnvelope struct {
	Body struct {
		DeleteFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string `xml:"ResponseCode"`
				} `xml:"DeleteFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"DeleteFolderResponse"`
	} `xml:"Body"`
}

type findTasksFolderEnvelope struct {
	Body struct {
		FindFolderResponse struct {
			ResponseMessages struct {
				Message struct {
					ResponseCode string           `xml:"ResponseCode"`
					Root         findTasksRootXML `xml:"RootFolder"`
				} `xml:"FindFolderResponseMessage"`
			} `xml:"ResponseMessages"`
		} `xml:"FindFolderResponse"`
	} `xml:"Body"`
}

type findTasksRootXML struct {
	IndexedPagingOffset     int                 `xml:"IndexedPagingOffset,attr"`
	TotalItemsInView        int                 `xml:"TotalItemsInView,attr"`
	IncludesLastItemInRange bool                `xml:"IncludesLastItemInRange,attr"`
	Folders                 tasksFoldersWrapper `xml:"Folders"`
}

type tasksFoldersWrapper struct {
	Items []folderXML `xml:",any"`
}

func (w tasksFoldersWrapper) toFolders() []Folder {
	out := make([]Folder, 0, len(w.Items))
	for _, raw := range w.Items {
		out = append(out, raw.toFolder())
	}
	return out
}
