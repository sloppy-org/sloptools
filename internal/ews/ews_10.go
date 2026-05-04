package ews

import (
	"encoding/xml"
	"strings"
	"time"
)

type itemXML struct {
	XMLName        xml.Name        `xml:""`
	ItemID         folderIDXMLNode `xml:"ItemId"`
	MimeContent    string          `xml:"MimeContent"`
	ParentFolderID folderIDXMLNode `xml:"ParentFolderId"`
	ConversationID struct {
		ID string `xml:"Id,attr"`
	} `xml:"ConversationId"`
	Subject string `xml:"Subject"`
	Body    struct {
		Type  string `xml:"BodyType,attr"`
		Value string `xml:",chardata"`
	} `xml:"Body"`
	DisplayTo         string `xml:"DisplayTo"`
	DisplayCc         string `xml:"DisplayCc"`
	WebLink           string `xml:"WebClientReadFormQueryString"`
	InternetMessageID string `xml:"InternetMessageId"`
	DateTimeReceived  string `xml:"DateTimeReceived"`
	DateTimeSent      string `xml:"DateTimeSent"`
	DateTimeCreated   string `xml:"DateTimeCreated"`
	IsRead            string `xml:"IsRead"`
	IsDraft           string `xml:"IsDraft"`
	HasAttachments    string `xml:"HasAttachments"`
	ConversationTopic string `xml:"ConversationTopic"`
	From              struct {
		Mailbox mailboxXML `xml:"Mailbox"`
	} `xml:"From"`
	Sender struct {
		Mailbox mailboxXML `xml:"Mailbox"`
	} `xml:"Sender"`
	ToRecipients struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"ToRecipients"`
	CcRecipients struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"CcRecipients"`
	Flag struct {
		Status    string `xml:"FlagStatus"`
		StartDate string `xml:"StartDate"`
		DueDate   string `xml:"DueDate"`
	} `xml:"Flag"`
	Attachments struct {
		Files []attachmentXML `xml:"FileAttachment"`
	} `xml:"Attachments"`
	CompanyName    string `xml:"CompanyName"`
	EmailAddresses struct {
		Entries []contactEmailXML `xml:"Entry"`
	} `xml:"EmailAddresses"`
	PhoneNumbers struct {
		Entries []labeledStringXML `xml:"Entry"`
	} `xml:"PhoneNumbers"`
	PhysicalAddresses struct{} `xml:"PhysicalAddresses"`
	Location          string   `xml:"Location"`
	Start             string   `xml:"Start"`
	End               string   `xml:"End"`
	IsAllDayEvent     string   `xml:"IsAllDayEvent"`
	Status            string   `xml:"Status"`
	StartDate         string   `xml:"StartDate"`
	DueDate           string   `xml:"DueDate"`
	CompleteDate      string   `xml:"CompleteDate"`
	// Calendar-specific fields
	UID               string       `xml:"UID"`
	Organizer         organizerXML `xml:"Organizer"`
	RequiredAttendees struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"RequiredAttendees"`
	OptionalAttendees struct {
		Mailboxes []mailboxXML `xml:"Mailbox"`
	} `xml:"OptionalAttendees"`
	Recurrence string `xml:"Recurrence"`
}

type organizerXML struct {
	Mailbox mailboxXML `xml:"Mailbox"`
}

type attachmentXML struct {
	ID struct {
		ID string `xml:"Id,attr"`
	} `xml:"AttachmentId"`
	Name        string `xml:"Name"`
	ContentType string `xml:"ContentType"`
	Size        string `xml:"Size"`
	IsInline    string `xml:"IsInline"`
}

type attachmentContentXML struct {
	ID struct {
		ID string `xml:"Id,attr"`
	} `xml:"AttachmentId"`
	Name        string `xml:"Name"`
	ContentType string `xml:"ContentType"`
	Content     string `xml:"Content"`
	Size        string `xml:"Size"`
	IsInline    string `xml:"IsInline"`
}

type contactEmailXML struct {
	Key   string `xml:"Key,attr"`
	Name  string `xml:"Name"`
	Value string `xml:"EmailAddress"`
}

type labeledStringXML struct {
	Key   string `xml:"Key,attr"`
	Value string `xml:",chardata"`
}

func (i itemXML) toMessage() Message {
	attachments := make([]Attachment, 0, len(i.Attachments.Files))
	for _, attachment := range i.Attachments.Files {
		attachments = append(attachments, Attachment{ID: strings.TrimSpace(attachment.ID.ID), Name: strings.TrimSpace(attachment.Name), ContentType: strings.TrimSpace(attachment.ContentType), Size: parseInt64(attachment.Size), IsInline: parseBool(attachment.IsInline)})
	}
	flagDue := parseTime(i.Flag.DueDate)
	if flagDue.IsZero() {
		flagDue = parseTime(i.Flag.StartDate)
	}
	return Message{ID: strings.TrimSpace(i.ItemID.ID), ChangeKey: strings.TrimSpace(i.ItemID.ChangeKey), ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID), ConversationID: strings.TrimSpace(i.ConversationID.ID), ConversationTopic: strings.TrimSpace(i.ConversationTopic), InternetMessageID: strings.TrimSpace(i.InternetMessageID), Subject: strings.TrimSpace(i.Subject), Body: strings.TrimSpace(i.Body.Value), BodyType: strings.TrimSpace(i.Body.Type), From: i.From.Mailbox.toMailbox(), Sender: i.Sender.Mailbox.toMailbox(), To: mailboxSlice(i.ToRecipients.Mailboxes), Cc: mailboxSlice(i.CcRecipients.Mailboxes), DisplayTo: strings.TrimSpace(i.DisplayTo), DisplayCc: strings.TrimSpace(i.DisplayCc), WebLink: strings.TrimSpace(i.WebLink), IsRead: parseBool(i.IsRead), IsDraft: parseBool(i.IsDraft), HasAttachments: parseBool(i.HasAttachments), ReceivedAt: parseTime(i.DateTimeReceived), SentAt: parseTime(i.DateTimeSent), CreatedAt: parseTime(i.DateTimeCreated), FlagStatus: strings.TrimSpace(i.Flag.Status), FlagDueAt: flagDue, Attachments: attachments}
}

func (i itemXML) toContact() Contact {
	emailAddress := ""
	for _, entry := range i.EmailAddresses.Entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			emailAddress = clean
			break
		}
	}
	phones := make([]string, 0, len(i.PhoneNumbers.Entries))
	for _, entry := range i.PhoneNumbers.Entries {
		if clean := strings.TrimSpace(entry.Value); clean != "" {
			phones = append(phones, clean)
		}
	}
	return Contact{ID: strings.TrimSpace(i.ItemID.ID), ChangeKey: strings.TrimSpace(i.ItemID.ChangeKey), ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID), DisplayName: strings.TrimSpace(i.Subject), CompanyName: strings.TrimSpace(i.CompanyName), Email: emailAddress, Phones: phones}
}

func (i itemXML) toEvent() Event {
	ev := Event{
		ID:             strings.TrimSpace(i.ItemID.ID),
		ChangeKey:      strings.TrimSpace(i.ItemID.ChangeKey),
		ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID),
		Subject:        strings.TrimSpace(i.Subject),
		Body:           strings.TrimSpace(i.Body.Value),
		BodyType:       strings.TrimSpace(i.Body.Type),
		Location:       strings.TrimSpace(i.Location),
		Start:          parseTime(i.Start),
		End:            parseTime(i.End),
		IsAllDay:       parseBool(i.IsAllDayEvent),
		UID:            strings.TrimSpace(i.UID),
		Status:         strings.TrimSpace(i.Status),
		Recurrence:     strings.TrimSpace(i.Recurrence),
	}
	if clean := strings.TrimSpace(i.Organizer.Mailbox.Email); clean != "" {
		ev.Organizer = clean
	}
	for _, m := range i.RequiredAttendees.Mailboxes {
		ev.Attendees = append(ev.Attendees, EventAttendee{Email: m.Email, Name: m.Name, Required: true})
	}
	for _, m := range i.OptionalAttendees.Mailboxes {
		ev.Attendees = append(ev.Attendees, EventAttendee{Email: m.Email, Name: m.Name, Required: false})
	}
	return ev
}

func (i itemXML) toTask() Task {
	start := parseTime(i.StartDate)
	due := parseTime(i.DueDate)
	complete := parseTime(i.CompleteDate)
	return Task{ID: strings.TrimSpace(i.ItemID.ID), ChangeKey: strings.TrimSpace(i.ItemID.ChangeKey), ParentFolderID: strings.TrimSpace(i.ParentFolderID.ID), Subject: strings.TrimSpace(i.Subject), Body: strings.TrimSpace(i.Body.Value), BodyType: strings.TrimSpace(i.Body.Type), Status: strings.TrimSpace(i.Status), StartDate: timePtrIfSet(start), DueDate: timePtrIfSet(due), CompleteDate: timePtrIfSet(complete), IsComplete: strings.EqualFold(strings.TrimSpace(i.Status), "Completed") || !complete.IsZero()}
}

func timePtrIfSet(value time.Time) *time.Time {
	if value.IsZero() {
		return nil
	}
	v := value
	return &v
}
