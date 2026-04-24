package ews

import (
	"context"
	"encoding/base64"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
)

func (c *Client) SyncFolderItems(ctx context.Context, folderID, syncState string, maxChanges int) (SyncItemsResult, error) {
	if strings.TrimSpace(folderID) == "" {
		folderID = "inbox"
	}
	if maxChanges <= 0 {
		maxChanges = c.cfg.BatchSize
	}
	body := `<m:SyncFolderItems><m:ItemShape><t:BaseShape>IdOnly</t:BaseShape></m:ItemShape><m:SyncFolderId>` + folderIDXML(folderID) + `</m:SyncFolderId>`
	if clean := strings.TrimSpace(syncState); clean != "" {
		body += `<m:SyncState>` + xmlEscapeText(clean) + `</m:SyncState>`
	}
	body += `<m:MaxChangesReturned>` + strconv.Itoa(maxChanges) + `</m:MaxChangesReturned></m:SyncFolderItems>`
	var resp syncFolderItemsEnvelope
	if err := c.call(ctx, "SyncFolderItems", body, &resp); err != nil {
		return SyncItemsResult{}, err
	}
	msg := resp.Body.SyncFolderItemsResponse.ResponseMessages.Message
	out := SyncItemsResult{SyncState: strings.TrimSpace(msg.SyncState), IncludesLastItem: msg.IncludesLastItemInRange}
	for _, change := range msg.Changes.Values {
		itemID := strings.TrimSpace(change.ResolveItemID())
		if itemID == "" {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(change.XMLName.Local)) {
		case "delete":
			out.DeletedItemIDs = append(out.DeletedItemIDs, itemID)
		default:
			out.ItemIDs = append(out.ItemIDs, itemID)
		}
	}
	return out, nil
}

func (c *Client) GetMessages(ctx context.Context, ids []string) ([]Message, error) {
	return c.getMessages(ctx, ids, true)
}

func (c *Client) GetMessageSummaries(ctx context.Context, ids []string) ([]Message, error) {
	return c.getMessages(ctx, ids, false)
}

func (c *Client) GetAttachment(ctx context.Context, attachmentID string) (AttachmentContent, error) {
	cleanID := strings.TrimSpace(attachmentID)
	if cleanID == "" {
		return AttachmentContent{}, fmt.Errorf("attachment id is required")
	}
	var resp getAttachmentEnvelope
	if err := c.call(ctx, "GetAttachment", getAttachmentBody(cleanID), &resp); err != nil {
		return AttachmentContent{}, err
	}
	files := resp.Body.GetAttachmentResponse.ResponseMessages.Message.Attachments.Files
	if len(files) == 0 {
		return AttachmentContent{}, fmt.Errorf("attachment %q not found", cleanID)
	}
	raw := files[0]
	content := []byte{}
	if encoded := strings.TrimSpace(raw.Content); encoded != "" {
		decoded, err := base64.StdEncoding.DecodeString(encoded)
		if err != nil {
			return AttachmentContent{}, err
		}
		content = decoded
	}
	return AttachmentContent{ID: strings.TrimSpace(raw.ID.ID), Name: strings.TrimSpace(raw.Name), ContentType: strings.TrimSpace(raw.ContentType), Size: parseInt64(raw.Size), IsInline: parseBool(raw.IsInline), Content: content}, nil
}

func (c *Client) GetMessageMIME(ctx context.Context, id string) ([]byte, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, fmt.Errorf("message id is required")
	}
	var b strings.Builder
	b.WriteString(`<m:GetItem><m:ItemShape>`)
	b.WriteString(`<t:BaseShape>IdOnly</t:BaseShape>`)
	b.WriteString(`<t:IncludeMimeContent>true</t:IncludeMimeContent>`)
	b.WriteString(`</m:ItemShape><m:ItemIds>`)
	b.WriteString(`<t:ItemId Id="`)
	b.WriteString(xmlEscapeAttr(id))
	b.WriteString(`" />`)
	b.WriteString(`</m:ItemIds></m:GetItem>`)
	var resp getItemEnvelope
	if err := c.call(ctx, "GetItem", b.String(), &resp); err != nil {
		return nil, err
	}
	items := resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return nil, fmt.Errorf("message %q not found", id)
	}
	encoded := strings.TrimSpace(items[0].MimeContent)
	if encoded == "" {
		return nil, fmt.Errorf("message %q has no MIME content", id)
	}
	return base64.StdEncoding.DecodeString(encoded)
}

func (c *Client) CreateMessageInFolder(ctx context.Context, folderID string, mime []byte, markRead bool) (Message, error) {
	encoded := base64.StdEncoding.EncodeToString(mime)
	var b strings.Builder
	b.WriteString(`<m:CreateItem MessageDisposition="SaveOnly">`)
	b.WriteString(`<m:SavedItemFolderId>`)
	b.WriteString(folderIDXML(folderID))
	b.WriteString(`</m:SavedItemFolderId>`)
	b.WriteString(`<m:Items><t:Message>`)
	b.WriteString(`<t:MimeContent CharacterSet="UTF-8">`)
	b.WriteString(encoded)
	b.WriteString(`</t:MimeContent>`)
	if markRead {
		b.WriteString(`<t:IsRead>true</t:IsRead>`)
	}
	b.WriteString(`</t:Message></m:Items></m:CreateItem>`)
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", b.String(), &resp); err != nil {
		return Message{}, err
	}
	items := resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values
	if len(items) == 0 {
		return Message{}, fmt.Errorf("ews CreateItem returned no items")
	}
	return items[0].toMessage(), nil
}

func (c *Client) getMessages(ctx context.Context, ids []string, includeBody bool) ([]Message, error) {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	out := make([]Message, 0, len(ids))
	for start := 0; start < len(ids); start += c.cfg.BatchSize {
		end := start + c.cfg.BatchSize
		if end > len(ids) {
			end = len(ids)
		}
		var resp getItemEnvelope
		if err := c.call(ctx, "GetItem", getItemBody(ids[start:end], includeBody), &resp); err != nil {
			return nil, err
		}
		for _, raw := range resp.Body.GetItemResponse.ResponseMessages.Message.Items.Values {
			message := raw.toMessage()
			if !includeBody {
				message.Body = ""
				message.BodyType = ""
			}
			out = append(out, message)
		}
	}
	return out, nil
}

func (c *Client) GetContacts(ctx context.Context, folderID string, offset, max int) ([]Contact, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "contacts"), offset, max, "Contact")
	if err != nil {
		return nil, err
	}
	out := make([]Contact, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toContact())
	}
	return out, nil
}

func (c *Client) GetCalendarEvents(ctx context.Context, folderID string, offset, max int) ([]Event, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "calendar"), offset, max, "CalendarItem")
	if err != nil {
		return nil, err
	}
	out := make([]Event, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toEvent())
	}
	return out, nil
}

func (c *Client) GetTasks(ctx context.Context, folderID string, offset, max int) ([]Task, error) {
	rawItems, err := c.getTypedItems(ctx, folderIDOrDistinguished(folderID, "tasks"), offset, max, "Task")
	if err != nil {
		return nil, err
	}
	out := make([]Task, 0, len(rawItems))
	for _, raw := range rawItems {
		out = append(out, raw.toTask())
	}
	return out, nil
}

func (c *Client) GetInboxRules(ctx context.Context) ([]Rule, error) {
	var resp getInboxRulesEnvelope
	if err := c.call(ctx, "GetInboxRules", `<m:GetInboxRules />`, &resp); err != nil {
		return nil, err
	}
	out := make([]Rule, 0, len(resp.Body.GetInboxRulesResponse.Rules))
	for _, raw := range resp.Body.GetInboxRulesResponse.Rules {
		out = append(out, raw.toRule())
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Priority < out[j].Priority
	})
	return out, nil
}

func (c *Client) CreateDraft(ctx context.Context, message DraftMessage) (Message, error) {
	body := `<m:CreateItem MessageDisposition="SaveOnly"><m:SavedItemFolderId><t:DistinguishedFolderId Id="drafts" /></m:SavedItemFolderId><m:Items>` + draftMessageXML(message) + `</m:Items></m:CreateItem>`
	var resp createItemEnvelope
	if err := c.call(ctx, "CreateItem", body, &resp); err != nil {
		return Message{}, err
	}
	if len(resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values) == 0 {
		return Message{}, fmt.Errorf("ews CreateItem returned no items")
	}
	return resp.Body.CreateItemResponse.ResponseMessages.Message.Items.Values[0].toMessage(), nil
}

func (c *Client) CreateAttachment(ctx context.Context, parentID, parentChangeKey string, file AttachmentFile) (string, error) {
	parentID = strings.TrimSpace(parentID)
	if parentID == "" {
		return "", fmt.Errorf("parent item id is required")
	}
	contentType := strings.TrimSpace(file.ContentType)
	if contentType == "" {
		contentType = "application/octet-stream"
	}
	name := strings.TrimSpace(file.Name)
	if name == "" {
		name = "attachment"
	}
	var b strings.Builder
	b.WriteString(`<m:CreateAttachment><m:ParentItemId Id="`)
	b.WriteString(xmlEscapeAttr(parentID))
	b.WriteString(`"`)
	if ck := strings.TrimSpace(parentChangeKey); ck != "" {
		b.WriteString(` ChangeKey="`)
		b.WriteString(xmlEscapeAttr(ck))
		b.WriteString(`"`)
	}
	b.WriteString(` /><m:Attachments><t:FileAttachment><t:Name>`)
	b.WriteString(xmlEscapeText(name))
	b.WriteString(`</t:Name><t:ContentType>`)
	b.WriteString(xmlEscapeText(contentType))
	b.WriteString(`</t:ContentType>`)
	if file.IsInline {
		b.WriteString(`<t:IsInline>true</t:IsInline>`)
	}
	b.WriteString(`<t:Content>`)
	b.WriteString(base64.StdEncoding.EncodeToString(file.Content))
	b.WriteString(`</t:Content></t:FileAttachment></m:Attachments></m:CreateAttachment>`)
	var resp createAttachmentEnvelope
	if err := c.call(ctx, "CreateAttachment", b.String(), &resp); err != nil {
		return "", err
	}
	msg := resp.Body.CreateAttachmentResponse.ResponseMessages.Message
	if rc := strings.TrimSpace(msg.ResponseCode); rc != "" && !strings.EqualFold(rc, "NoError") {
		return "", fmt.Errorf("ews CreateAttachment: %s", rc)
	}
	if len(msg.Attachments.Files) == 0 {
		return "", fmt.Errorf("ews CreateAttachment returned no attachment")
	}
	newKey := strings.TrimSpace(msg.Attachments.Files[0].AttachmentID.RootItemChangeKey)
	if newKey == "" {
		newKey = parentChangeKey
	}
	return newKey, nil
} // CreateAttachment adds one file attachment to an existing item (typically a
// draft) and returns the new parent ChangeKey. Call it once per file, reusing
// the returned ChangeKey as the parentChangeKey for subsequent calls, since
// each attachment write invalidates the previous key. Splitting large payloads
// across separate CreateAttachment SOAP calls avoids the bare-401 that
// TU Graz Exchange returns for oversized CreateItem bodies.

func (c *Client) UpdateDraft(ctx context.Context, itemID string, message DraftMessage) (Message, error) {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return Message{}, fmt.Errorf("draft id is required")
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges><t:ItemChange><t:ItemId Id="` + xmlEscapeAttr(itemID) + `" /><t:Updates>` + setDraftMimeContentXML(message) + `</t:Updates></t:ItemChange></m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	if err := c.call(ctx, "UpdateItem", body, &resp); err != nil {
		return Message{}, err
	}
	items, err := c.GetMessages(ctx, []string{itemID})
	if err != nil {
		return Message{}, err
	}
	if len(items) == 0 {
		return Message{}, fmt.Errorf("draft %q not found after update", itemID)
	}
	return items[0], nil
}

func (c *Client) SendDraft(ctx context.Context, itemID string) error {
	itemID = strings.TrimSpace(itemID)
	if itemID == "" {
		return fmt.Errorf("draft id is required")
	}
	body := `<m:SendItem SaveItemToFolder="true"><m:ItemIds><t:ItemId Id="` + xmlEscapeAttr(itemID) + `" /></m:ItemIds><m:SavedItemFolderId><t:DistinguishedFolderId Id="sentitems" /></m:SavedItemFolderId></m:SendItem>`
	var resp simpleResponseEnvelope
	return c.call(ctx, "SendItem", body, &resp)
}

func (c *Client) UpdateInboxRules(ctx context.Context, operations []RuleOperation) error {
	opsXML, err := ruleOperationsXML(operations)
	if err != nil {
		return err
	}
	body := `<m:UpdateInboxRules><m:RemoveOutlookRuleBlob>true</m:RemoveOutlookRuleBlob><m:Operations>` + opsXML + `</m:Operations></m:UpdateInboxRules>`
	var resp updateInboxRulesEnvelope
	return c.call(ctx, "UpdateInboxRules", body, &resp)
}

func (c *Client) MoveItems(ctx context.Context, ids []string, folderID string) ([]string, error) {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil, nil
	}
	body := `<m:MoveItem><m:ToFolderId>` + folderIDXML(folderID) + `</m:ToFolderId><m:ItemIds>` + itemIDsXML(ids) + `</m:ItemIds></m:MoveItem>`
	var resp moveItemEnvelope
	if err := c.call(ctx, "MoveItem", body, &resp); err != nil {
		return nil, err
	}
	return resp.ResolvedItemIDs(), nil
}

func (c *Client) DeleteItems(ctx context.Context, ids []string, hardDelete bool) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	deleteType := "MoveToDeletedItems"
	if hardDelete {
		deleteType = "HardDelete"
	}
	body := fmt.Sprintf(`<m:DeleteItem DeleteType="%s" SendMeetingCancellations="SendToNone" AffectedTaskOccurrences="AllOccurrences"><m:ItemIds>%s</m:ItemIds></m:DeleteItem>`, deleteType, itemIDsXML(ids))
	var resp simpleResponseEnvelope
	return c.call(ctx, "DeleteItem", body, &resp)
}

func (c *Client) SetReadState(ctx context.Context, ids []string, isRead bool) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	value := "false"
	if isRead {
		value = "true"
	}
	var changes strings.Builder
	for _, id := range ids {
		changes.WriteString(`<t:ItemChange><t:ItemId Id="`)
		changes.WriteString(xmlEscapeAttr(id))
		changes.WriteString(`" /><t:Updates><t:SetItemField><t:FieldURI FieldURI="message:IsRead" /><t:Message><t:IsRead>`)
		changes.WriteString(value)
		changes.WriteString(`</t:IsRead></t:Message></t:SetItemField></t:Updates></t:ItemChange>`)
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>` + changes.String() + `</m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	return c.call(ctx, "UpdateItem", body, &resp)
}

const (
	FlagStatusNotFlagged = "NotFlagged"
	FlagStatusFlagged    = "Flagged"
	FlagStatusComplete   = "Complete"
) // FlagStatus values understood by SetFlag. EWS stores the follow-up state
// alongside an optional DueDate in the item:Flag complex property.

func (c *Client) SetFlag(ctx context.Context, ids []string, // SetFlag writes the follow-up flag state on one or more items. dueAt is
	// optional and only honoured when status is Flagged. A zero time skips the
	// DueDateTime field.
	status string, dueAt time.Time) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	status = strings.TrimSpace(status)
	if status == "" {
		return fmt.Errorf("ews SetFlag: status is required")
	}
	var changes strings.Builder
	for _, id := range ids {
		changes.WriteString(`<t:ItemChange><t:ItemId Id="`)
		changes.WriteString(xmlEscapeAttr(id))
		changes.WriteString(`" /><t:Updates><t:SetItemField><t:FieldURI FieldURI="item:Flag" /><t:Item><t:Flag><t:FlagStatus>`)
		changes.WriteString(xmlEscapeAttr(status))
		changes.WriteString(`</t:FlagStatus>`)
		if status == FlagStatusFlagged && !dueAt.IsZero() {
			changes.WriteString(`<t:StartDate>`)
			changes.WriteString(dueAt.UTC().Format(time.RFC3339))
			changes.WriteString(`</t:StartDate><t:DueDate>`)
			changes.WriteString(dueAt.UTC().Format(time.RFC3339))
			changes.WriteString(`</t:DueDate>`)
		}
		changes.WriteString(`</t:Flag></t:Item></t:SetItemField></t:Updates></t:ItemChange>`)
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>` + changes.String() + `</m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	return c.call(ctx, "UpdateItem", body, &resp)
}

func (c *Client) SetCategories(ctx context.Context, ids []string, // SetCategories replaces the categories collection on each item. An empty
	// categories slice clears the collection via DeleteItemField.
	categories []string) error {
	ids = compactStrings(ids)
	if len(ids) == 0 {
		return nil
	}
	clean := compactStrings(categories)
	var changes strings.Builder
	for _, id := range ids {
		changes.WriteString(`<t:ItemChange><t:ItemId Id="`)
		changes.WriteString(xmlEscapeAttr(id))
		changes.WriteString(`" /><t:Updates>`)
		if len(clean) == 0 {
			changes.WriteString(`<t:DeleteItemField><t:FieldURI FieldURI="item:Categories" /></t:DeleteItemField>`)
		} else {
			changes.WriteString(`<t:SetItemField><t:FieldURI FieldURI="item:Categories" /><t:Item><t:Categories>`)
			for _, category := range clean {
				changes.WriteString(`<t:String>`)
				changes.WriteString(xmlEscapeText(category))
				changes.WriteString(`</t:String>`)
			}
			changes.WriteString(`</t:Categories></t:Item></t:SetItemField>`)
		}
		changes.WriteString(`</t:Updates></t:ItemChange>`)
	}
	body := `<m:UpdateItem MessageDisposition="SaveOnly" ConflictResolution="AlwaysOverwrite"><m:ItemChanges>` + changes.String() + `</m:ItemChanges></m:UpdateItem>`
	var resp updateItemEnvelope
	return c.call(ctx, "UpdateItem", body, &resp)
}

func (c *Client) FindFolderByName(ctx context.Context, name string) (*Folder, error) {
	target := strings.ToLower(strings.TrimSpace(name))
	if target == "" {
		return nil, nil
	}
	folders, err := c.ListFolders(ctx)
	if err != nil {
		return nil, err
	}
	for i := range folders {
		if strings.EqualFold(strings.TrimSpace(folders[i].Name), target) {
			return &folders[i], nil
		}
	}
	return nil, nil
}

func (c *Client) Watch(ctx context.Context, opts WatchOptions, onEvents func(StreamBatch) error) error {
	if onEvents == nil {
		return fmt.Errorf("ews watch callback is required")
	}
	subscriptionID, err := c.subscribeStreaming(ctx, opts)
	if err != nil {
		return err
	}
	connectionTimeout := streamingConnectionTimeoutMinutes(opts.ConnectionTimeout)
	for {
		batch, err := c.getStreamingEvents(ctx, subscriptionID, connectionTimeout)
		if err != nil {
			return err
		}
		if len(batch.Events) == 0 {
			continue
		}
		if err := onEvents(batch); err != nil {
			return err
		}
	}
}
