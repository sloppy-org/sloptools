package ews

import (
	"context"
	"fmt"
	"strings"
	"time"
)

// OofState mirrors the EWS `OofState` enumeration. Disabled means no
// auto-reply, Enabled means always-on, Scheduled means active only inside the
// optional Duration window.
type OofState string

const (
	OofStateDisabled  OofState = "Disabled"
	OofStateEnabled   OofState = "Enabled"
	OofStateScheduled OofState = "Scheduled"
)

// OofExternalAudience mirrors the EWS `ExternalAudience` enumeration. None
// only replies to internal senders, Known replies to contacts, All replies to
// every external sender.
type OofExternalAudience string

const (
	OofAudienceNone  OofExternalAudience = "None"
	OofAudienceKnown OofExternalAudience = "Known"
	OofAudienceAll   OofExternalAudience = "All"
)

// OofSettings is the canonical EWS-side representation of the user's
// out-of-office configuration. The mailboxsettings package converts it to and
// from providerdata.OOFSettings.
type OofSettings struct {
	State            OofState
	ExternalAudience OofExternalAudience
	Start            time.Time
	End              time.Time
	InternalReply    string
	ExternalReply    string
}

// GetUserOofSettings invokes the EWS `GetUserOofSettings` SOAP operation for
// the given mailbox SMTP address.
func (c *Client) GetUserOofSettings(ctx context.Context, mailbox string) (OofSettings, error) {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return OofSettings{}, fmt.Errorf("ews GetUserOofSettings: mailbox is required")
	}
	body := `<m:GetUserOofSettingsRequest><t:Mailbox><t:Address>` + xmlEscapeText(mailbox) + `</t:Address></t:Mailbox></m:GetUserOofSettingsRequest>`
	var resp getUserOofSettingsEnvelope
	if err := c.call(ctx, "GetUserOofSettings", body, &resp); err != nil {
		return OofSettings{}, err
	}
	return resp.toOofSettings(), nil
}

// SetUserOofSettings invokes the EWS `SetUserOofSettings` SOAP operation for
// the given mailbox SMTP address. State==Disabled clears the responder; the
// Duration window is only emitted when State==Scheduled and both endpoints are
// set, matching the EWS schema requirement.
func (c *Client) SetUserOofSettings(ctx context.Context, mailbox string, settings OofSettings) error {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return fmt.Errorf("ews SetUserOofSettings: mailbox is required")
	}
	body := `<m:SetUserOofSettingsRequest><t:Mailbox><t:Address>` + xmlEscapeText(mailbox) + `</t:Address></t:Mailbox><t:UserOofSettings>` + userOofSettingsXML(settings) + `</t:UserOofSettings></m:SetUserOofSettingsRequest>`
	var resp setUserOofSettingsEnvelope
	return c.call(ctx, "SetUserOofSettings", body, &resp)
}

func userOofSettingsXML(settings OofSettings) string {
	state := normalizeOofState(settings.State)
	audience := normalizeOofAudience(settings.ExternalAudience)
	var b strings.Builder
	b.WriteString(`<t:OofState>`)
	b.WriteString(string(state))
	b.WriteString(`</t:OofState>`)
	b.WriteString(`<t:ExternalAudience>`)
	b.WriteString(string(audience))
	b.WriteString(`</t:ExternalAudience>`)
	if state == OofStateScheduled && !settings.Start.IsZero() && !settings.End.IsZero() {
		b.WriteString(`<t:Duration><t:StartTime>`)
		b.WriteString(settings.Start.UTC().Format(time.RFC3339))
		b.WriteString(`</t:StartTime><t:EndTime>`)
		b.WriteString(settings.End.UTC().Format(time.RFC3339))
		b.WriteString(`</t:EndTime></t:Duration>`)
	}
	b.WriteString(`<t:InternalReply><t:Message>`)
	b.WriteString(xmlEscapeText(settings.InternalReply))
	b.WriteString(`</t:Message></t:InternalReply>`)
	b.WriteString(`<t:ExternalReply><t:Message>`)
	b.WriteString(xmlEscapeText(settings.ExternalReply))
	b.WriteString(`</t:Message></t:ExternalReply>`)
	return b.String()
}

func normalizeOofState(state OofState) OofState {
	switch OofState(strings.ToLower(strings.TrimSpace(string(state)))) {
	case "enabled":
		return OofStateEnabled
	case "scheduled":
		return OofStateScheduled
	default:
		return OofStateDisabled
	}
}

func normalizeOofAudience(audience OofExternalAudience) OofExternalAudience {
	switch OofExternalAudience(strings.ToLower(strings.TrimSpace(string(audience)))) {
	case "all":
		return OofAudienceAll
	case "known":
		return OofAudienceKnown
	default:
		return OofAudienceNone
	}
}

type getUserOofSettingsEnvelope struct {
	Body struct {
		Response struct {
			ResponseMessage struct {
				ResponseCode string `xml:"ResponseCode"`
				MessageText  string `xml:"MessageText"`
			} `xml:"ResponseMessage"`
			OofSettings oofSettingsXML `xml:"OofSettings"`
		} `xml:"GetUserOofSettingsResponse"`
	} `xml:"Body"`
}

func (e getUserOofSettingsEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.Response.ResponseMessage.ResponseCode)
}

func (e getUserOofSettingsEnvelope) toOofSettings() OofSettings {
	raw := e.Body.Response.OofSettings
	out := OofSettings{
		State:            normalizeOofState(OofState(strings.TrimSpace(raw.OofState))),
		ExternalAudience: normalizeOofAudience(OofExternalAudience(strings.TrimSpace(raw.ExternalAudience))),
		InternalReply:    strings.TrimSpace(raw.InternalReply.Message),
		ExternalReply:    strings.TrimSpace(raw.ExternalReply.Message),
	}
	if start := parseTime(raw.Duration.StartTime); !start.IsZero() {
		out.Start = start
	}
	if end := parseTime(raw.Duration.EndTime); !end.IsZero() {
		out.End = end
	}
	return out
}

type oofSettingsXML struct {
	OofState         string `xml:"OofState"`
	ExternalAudience string `xml:"ExternalAudience"`
	Duration         struct {
		StartTime string `xml:"StartTime"`
		EndTime   string `xml:"EndTime"`
	} `xml:"Duration"`
	InternalReply struct {
		Message string `xml:"Message"`
	} `xml:"InternalReply"`
	ExternalReply struct {
		Message string `xml:"Message"`
	} `xml:"ExternalReply"`
}

type setUserOofSettingsEnvelope struct {
	Body struct {
		Response struct {
			ResponseMessage struct {
				ResponseCode string `xml:"ResponseCode"`
				MessageText  string `xml:"MessageText"`
			} `xml:"ResponseMessage"`
		} `xml:"SetUserOofSettingsResponse"`
	} `xml:"Body"`
}

func (e setUserOofSettingsEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.Response.ResponseMessage.ResponseCode)
}

// DelegateUser is one entry returned by GetDelegate, carrying the delegate's
// identity and per-folder permission levels. Exchange reports permissions as
// enumerated strings (None, Reviewer, Author, Editor, Custom, ...); callers
// should treat them as opaque tokens suitable for display.
type DelegateUser struct {
	PrimarySmtpAddress string
	DisplayName        string
	Permissions        DelegatePermissions
	ViewPrivateItems   bool
	ReceiveCopiesOfMR  bool
}

// DelegatePermissions mirrors the EWS DelegatePermissions element. Each field
// is the raw permission-level string per Exchange folder; an empty value means
// the server omitted that folder (commonly "None").
type DelegatePermissions struct {
	CalendarFolder string
	TasksFolder    string
	InboxFolder    string
	ContactsFolder string
	NotesFolder    string
	JournalFolder  string
}

// GetDelegate invokes the EWS `GetDelegate` SOAP operation for the given
// mailbox SMTP address and returns the delegate users Exchange reports. The
// request includes IncludePermissions="true" so each delegate carries its
// per-folder permission levels.
func (c *Client) GetDelegate(ctx context.Context, mailbox string) ([]DelegateUser, error) {
	mailbox = strings.TrimSpace(mailbox)
	if mailbox == "" {
		return nil, fmt.Errorf("ews GetDelegate: mailbox is required")
	}
	body := `<m:GetDelegate IncludePermissions="true"><m:Mailbox><t:EmailAddress>` + xmlEscapeText(mailbox) + `</t:EmailAddress></m:Mailbox></m:GetDelegate>`
	var resp getDelegateEnvelope
	if err := c.call(ctx, "GetDelegate", body, &resp); err != nil {
		return nil, err
	}
	return resp.delegates(), nil
}

type getDelegateEnvelope struct {
	Body struct {
		Response struct {
			ResponseCode     string `xml:"ResponseCode"`
			MessageText      string `xml:"MessageText"`
			ResponseMessages struct {
				DelegateUserResponseMessageType []delegateUserResponseMessageXML `xml:"DelegateUserResponseMessageType"`
			} `xml:"ResponseMessages"`
			DeliverMeetingRequests string `xml:"DeliverMeetingRequests"`
		} `xml:"GetDelegateResponse"`
	} `xml:"Body"`
}

func (e getDelegateEnvelope) responseCode() string {
	return strings.TrimSpace(e.Body.Response.ResponseCode)
}

type delegateUserResponseMessageXML struct {
	ResponseClass string          `xml:"ResponseClass,attr"`
	ResponseCode  string          `xml:"ResponseCode"`
	DelegateUser  delegateUserXML `xml:"DelegateUser"`
}

type delegateUserXML struct {
	UserID struct {
		PrimarySmtpAddress string `xml:"PrimarySmtpAddress"`
		DisplayName        string `xml:"DisplayName"`
	} `xml:"UserId"`
	DelegatePermissions struct {
		CalendarFolder string `xml:"CalendarFolderPermissionLevel"`
		TasksFolder    string `xml:"TasksFolderPermissionLevel"`
		InboxFolder    string `xml:"InboxFolderPermissionLevel"`
		ContactsFolder string `xml:"ContactsFolderPermissionLevel"`
		NotesFolder    string `xml:"NotesFolderPermissionLevel"`
		JournalFolder  string `xml:"JournalFolderPermissionLevel"`
	} `xml:"DelegatePermissions"`
	ReceiveCopiesOfMeetingMessages string `xml:"ReceiveCopiesOfMeetingMessages"`
	ViewPrivateItems               string `xml:"ViewPrivateItems"`
}

func (e getDelegateEnvelope) delegates() []DelegateUser {
	messages := e.Body.Response.ResponseMessages.DelegateUserResponseMessageType
	out := make([]DelegateUser, 0, len(messages))
	for _, msg := range messages {
		if !strings.EqualFold(strings.TrimSpace(msg.ResponseClass), "Success") {
			continue
		}
		raw := msg.DelegateUser
		email := strings.TrimSpace(raw.UserID.PrimarySmtpAddress)
		name := strings.TrimSpace(raw.UserID.DisplayName)
		if email == "" && name == "" {
			continue
		}
		out = append(out, DelegateUser{
			PrimarySmtpAddress: email,
			DisplayName:        name,
			Permissions: DelegatePermissions{
				CalendarFolder: strings.TrimSpace(raw.DelegatePermissions.CalendarFolder),
				TasksFolder:    strings.TrimSpace(raw.DelegatePermissions.TasksFolder),
				InboxFolder:    strings.TrimSpace(raw.DelegatePermissions.InboxFolder),
				ContactsFolder: strings.TrimSpace(raw.DelegatePermissions.ContactsFolder),
				NotesFolder:    strings.TrimSpace(raw.DelegatePermissions.NotesFolder),
				JournalFolder:  strings.TrimSpace(raw.DelegatePermissions.JournalFolder),
			},
			ReceiveCopiesOfMR: strings.EqualFold(strings.TrimSpace(raw.ReceiveCopiesOfMeetingMessages), "true"),
			ViewPrivateItems:  strings.EqualFold(strings.TrimSpace(raw.ViewPrivateItems), "true"),
		})
	}
	return out
}
