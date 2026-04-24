package ews

import (
	"fmt"
	"strconv"
	"strings"
)

func (m streamingResponseMessageXML) toBatch() StreamBatch {
	out := StreamBatch{}
	for _, notification := range m.Notifications {
		batch := notification.toBatch()
		if out.SubscriptionID == "" {
			out.SubscriptionID = batch.SubscriptionID
		}
		if out.PreviousWatermark == "" {
			out.PreviousWatermark = batch.PreviousWatermark
		}
		out.MoreEvents = out.MoreEvents || batch.MoreEvents
		out.Events = append(out.Events, batch.Events...)
	}
	return out
}

type streamingNotificationXML struct {
	SubscriptionID    string              `xml:"SubscriptionId"`
	PreviousWatermark string              `xml:"PreviousWatermark"`
	MoreEvents        string              `xml:"MoreEvents"`
	CopiedEvents      []streamingEventXML `xml:"CopiedEvent"`
	CreatedEvents     []streamingEventXML `xml:"CreatedEvent"`
	DeletedEvents     []streamingEventXML `xml:"DeletedEvent"`
	FreeBusyEvents    []streamingEventXML `xml:"FreeBusyChangedEvent"`
	ModifiedEvents    []streamingEventXML `xml:"ModifiedEvent"`
	MovedEvents       []streamingEventXML `xml:"MovedEvent"`
	NewMailEvents     []streamingEventXML `xml:"NewMailEvent"`
}

func (n streamingNotificationXML) toBatch() StreamBatch {
	out := StreamBatch{SubscriptionID: strings.TrimSpace(n.SubscriptionID), PreviousWatermark: strings.TrimSpace(n.PreviousWatermark), MoreEvents: parseBool(n.MoreEvents)}
	out.Events = append(out.Events, appendStreamingEvents("copied", n.CopiedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("created", n.CreatedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("deleted", n.DeletedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("free_busy_changed", n.FreeBusyEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("modified", n.ModifiedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("moved", n.MovedEvents)...)
	out.Events = append(out.Events, appendStreamingEvents("new_mail", n.NewMailEvents)...)
	return out
}

type streamingEventXML struct {
	Watermark         string          `xml:"Watermark"`
	ItemID            folderIDXMLNode `xml:"ItemId"`
	OldItemID         folderIDXMLNode `xml:"OldItemId"`
	FolderID          folderIDXMLNode `xml:"FolderId"`
	ParentFolderID    folderIDXMLNode `xml:"ParentFolderId"`
	OldParentFolderID folderIDXMLNode `xml:"OldParentFolderId"`
}

func appendStreamingEvents(eventType string, values []streamingEventXML) []StreamEvent {
	out := make([]StreamEvent, 0, len(values))
	for _, value := range values {
		out = append(out, StreamEvent{Type: eventType, ItemID: strings.TrimSpace(value.ItemID.ID), OldItemID: strings.TrimSpace(value.OldItemID.ID), FolderID: strings.TrimSpace(value.FolderID.ID), ParentFolderID: strings.TrimSpace(value.ParentFolderID.ID), OldParentFolderID: strings.TrimSpace(value.OldParentFolderID.ID), Watermark: strings.TrimSpace(value.Watermark)})
	}
	return out
}

type ruleXML struct {
	ID         string            `xml:"RuleId"`
	Name       string            `xml:"DisplayName"`
	Priority   string            `xml:"Priority"`
	IsEnabled  string            `xml:"IsEnabled"`
	Conditions ruleConditionsXML `xml:"Conditions"`
	Exceptions ruleConditionsXML `xml:"Exceptions"`
	Actions    ruleActionsXML    `xml:"Actions"`
}

type ruleConditionsXML struct {
	ContainsSenderStrings  []string     `xml:"ContainsSenderStrings>String"`
	ContainsSubjectStrings []string     `xml:"ContainsSubjectStrings>String"`
	FromAddresses          []mailboxXML `xml:"FromAddresses>Address"`
	SentToAddresses        []mailboxXML `xml:"SentToAddresses>Address"`
	NotSentToMe            string       `xml:"NotSentToMe"`
	SentCcMe               string       `xml:"SentCcMe"`
}

type ruleActionsXML struct {
	Delete              string `xml:"Delete"`
	MarkAsRead          string `xml:"MarkAsRead"`
	StopProcessingRules string `xml:"StopProcessingRules"`
	MoveToFolder        struct {
		FolderID folderIDXMLNode `xml:"FolderId"`
	} `xml:"MoveToFolder"`
	RedirectToRecipients []mailboxXML `xml:"RedirectToRecipients>Address"`
}

func (r ruleXML) toRule() Rule {
	return Rule{ID: strings.TrimSpace(r.ID), Name: strings.TrimSpace(r.Name), Priority: parseInt(r.Priority), Enabled: parseBool(r.IsEnabled), Conditions: RuleConditions{ContainsSenderStrings: compactStrings(r.Conditions.ContainsSenderStrings), ContainsSubjectStrings: compactStrings(r.Conditions.ContainsSubjectStrings), FromAddresses: mailboxSlice(r.Conditions.FromAddresses), SentToAddresses: mailboxSlice(r.Conditions.SentToAddresses), NotSentToMe: parseBool(r.Conditions.NotSentToMe), SentCcMe: parseBool(r.Conditions.SentCcMe)}, Exceptions: RuleConditions{ContainsSenderStrings: compactStrings(r.Exceptions.ContainsSenderStrings), ContainsSubjectStrings: compactStrings(r.Exceptions.ContainsSubjectStrings), FromAddresses: mailboxSlice(r.Exceptions.FromAddresses), SentToAddresses: mailboxSlice(r.Exceptions.SentToAddresses), NotSentToMe: parseBool(r.Exceptions.NotSentToMe), SentCcMe: parseBool(r.Exceptions.SentCcMe)}, Actions: RuleActions{Delete: parseBool(r.Actions.Delete), MarkAsRead: parseBool(r.Actions.MarkAsRead), StopProcessingRules: parseBool(r.Actions.StopProcessingRules), MoveToFolderID: strings.TrimSpace(r.Actions.MoveToFolder.FolderID.ID), RedirectToRecipients: mailboxSlice(r.Actions.RedirectToRecipients)}}
}

func ruleOperationsXML(operations []RuleOperation) (string, error) {
	var b strings.Builder
	for _, operation := range operations {
		switch operation.Kind {
		case RuleOperationCreate:
			b.WriteString(`<t:CreateRuleOperation><t:Rule>`)
			b.WriteString(ruleXMLBody(operation.Rule, false))
			b.WriteString(`</t:Rule></t:CreateRuleOperation>`)
		case RuleOperationSet:
			if strings.TrimSpace(operation.Rule.ID) == "" {
				return "", fmt.Errorf("rule id is required for set operation")
			}
			b.WriteString(`<t:SetRuleOperation><t:Rule>`)
			b.WriteString(ruleXMLBody(operation.Rule, true))
			b.WriteString(`</t:Rule></t:SetRuleOperation>`)
		case RuleOperationDelete:
			if strings.TrimSpace(operation.Rule.ID) == "" {
				return "", fmt.Errorf("rule id is required for delete operation")
			}
			b.WriteString(`<t:DeleteRuleOperation><t:RuleId>`)
			b.WriteString(xmlEscapeText(strings.TrimSpace(operation.Rule.ID)))
			b.WriteString(`</t:RuleId></t:DeleteRuleOperation>`)
		default:
			return "", fmt.Errorf("unsupported rule operation %q", operation.Kind)
		}
	}
	return b.String(), nil
}

func ruleXMLBody(rule Rule, includeID bool) string {
	var b strings.Builder
	if includeID && strings.TrimSpace(rule.ID) != "" {
		b.WriteString(`<t:RuleId>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(rule.ID)))
		b.WriteString(`</t:RuleId>`)
	}
	b.WriteString(`<t:DisplayName>`)
	b.WriteString(xmlEscapeText(strings.TrimSpace(rule.Name)))
	b.WriteString(`</t:DisplayName><t:Priority>`)
	b.WriteString(strconv.Itoa(rule.Priority))
	b.WriteString(`</t:Priority><t:IsEnabled>`)
	if rule.Enabled {
		b.WriteString(`true`)
	} else {
		b.WriteString(`false`)
	}
	b.WriteString(`</t:IsEnabled>`)
	b.WriteString(ruleConditionsBodyXML(rule.Conditions))
	b.WriteString(ruleExceptionsBodyXML(rule.Exceptions))
	b.WriteString(ruleActionsBodyXML(rule.Actions))
	return b.String()
}

func ruleConditionsBodyXML(conditions RuleConditions) string {
	var b strings.Builder
	b.WriteString(`<t:Conditions>`)
	if len(conditions.ContainsSenderStrings) > 0 {
		b.WriteString(`<t:ContainsSenderStrings>`)
		for _, value := range compactStrings(conditions.ContainsSenderStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSenderStrings>`)
	}
	if len(conditions.ContainsSubjectStrings) > 0 {
		b.WriteString(`<t:ContainsSubjectStrings>`)
		for _, value := range compactStrings(conditions.ContainsSubjectStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSubjectStrings>`)
	}
	if len(conditions.FromAddresses) > 0 {
		b.WriteString(`<t:FromAddresses>`)
		for _, mailbox := range conditions.FromAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:FromAddresses>`)
	}
	if len(conditions.SentToAddresses) > 0 {
		b.WriteString(`<t:SentToAddresses>`)
		for _, mailbox := range conditions.SentToAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:SentToAddresses>`)
	}
	if conditions.NotSentToMe {
		b.WriteString(`<t:NotSentToMe>true</t:NotSentToMe>`)
	}
	if conditions.SentCcMe {
		b.WriteString(`<t:SentCcMe>true</t:SentCcMe>`)
	}
	b.WriteString(`</t:Conditions>`)
	return b.String()
}

func ruleExceptionsBodyXML(conditions RuleConditions) string {
	if len(conditions.ContainsSenderStrings) == 0 && len(conditions.ContainsSubjectStrings) == 0 && len(conditions.FromAddresses) == 0 && len(conditions.SentToAddresses) == 0 && !conditions.NotSentToMe && !conditions.SentCcMe {
		return `<t:Exceptions />`
	}
	var b strings.Builder
	b.WriteString(`<t:Exceptions>`)
	if len(conditions.ContainsSenderStrings) > 0 {
		b.WriteString(`<t:ContainsSenderStrings>`)
		for _, value := range compactStrings(conditions.ContainsSenderStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSenderStrings>`)
	}
	if len(conditions.ContainsSubjectStrings) > 0 {
		b.WriteString(`<t:ContainsSubjectStrings>`)
		for _, value := range compactStrings(conditions.ContainsSubjectStrings) {
			b.WriteString(`<t:String>`)
			b.WriteString(xmlEscapeText(value))
			b.WriteString(`</t:String>`)
		}
		b.WriteString(`</t:ContainsSubjectStrings>`)
	}
	if len(conditions.FromAddresses) > 0 {
		b.WriteString(`<t:FromAddresses>`)
		for _, mailbox := range conditions.FromAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:FromAddresses>`)
	}
	if len(conditions.SentToAddresses) > 0 {
		b.WriteString(`<t:SentToAddresses>`)
		for _, mailbox := range conditions.SentToAddresses {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:SentToAddresses>`)
	}
	if conditions.NotSentToMe {
		b.WriteString(`<t:NotSentToMe>true</t:NotSentToMe>`)
	}
	if conditions.SentCcMe {
		b.WriteString(`<t:SentCcMe>true</t:SentCcMe>`)
	}
	b.WriteString(`</t:Exceptions>`)
	return b.String()
}

func ruleActionsBodyXML(actions RuleActions) string {
	var b strings.Builder
	b.WriteString(`<t:Actions>`)
	if actions.Delete {
		b.WriteString(`<t:Delete>true</t:Delete>`)
	}
	if actions.MarkAsRead {
		b.WriteString(`<t:MarkAsRead>true</t:MarkAsRead>`)
	}
	if actions.StopProcessingRules {
		b.WriteString(`<t:StopProcessingRules>true</t:StopProcessingRules>`)
	}
	if folderXML := folderRefXML(actions.MoveToFolderID); folderXML != "" {
		b.WriteString(`<t:MoveToFolder>`)
		b.WriteString(folderXML)
		b.WriteString(`</t:MoveToFolder>`)
	}
	if len(actions.RedirectToRecipients) > 0 {
		b.WriteString(`<t:RedirectToRecipients>`)
		for _, mailbox := range actions.RedirectToRecipients {
			b.WriteString(mailboxAddressXML(mailbox))
		}
		b.WriteString(`</t:RedirectToRecipients>`)
	}
	b.WriteString(`</t:Actions>`)
	return b.String()
}

func mailboxAddressXML(mailbox Mailbox) string {
	var b strings.Builder
	b.WriteString(`<t:Address>`)
	if strings.TrimSpace(mailbox.Name) != "" {
		b.WriteString(`<t:Name>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.Name)))
		b.WriteString(`</t:Name>`)
	}
	if strings.TrimSpace(mailbox.Email) != "" {
		b.WriteString(`<t:EmailAddress>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.Email)))
		b.WriteString(`</t:EmailAddress>`)
	}
	if strings.TrimSpace(mailbox.RoutingType) != "" {
		b.WriteString(`<t:RoutingType>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.RoutingType)))
		b.WriteString(`</t:RoutingType>`)
	}
	if strings.TrimSpace(mailbox.MailboxType) != "" {
		b.WriteString(`<t:MailboxType>`)
		b.WriteString(xmlEscapeText(strings.TrimSpace(mailbox.MailboxType)))
		b.WriteString(`</t:MailboxType>`)
	}
	b.WriteString(`</t:Address>`)
	return b.String()
}
