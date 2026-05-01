package ews

import (
	"strings"
	"time"
)

// senderStringPropertyTags lists the MAPI extended-property tags whose String
// values hold the sender's address or display name. EWS exposes message:From
// as a structured Mailbox element, so <t:Contains> against it never matches a
// substring; matching the underlying string properties does.
var senderStringPropertyTags = []string{
	"0x0C1F", // PR_SENDER_EMAIL_ADDRESS_W
	"0x0C1A", // PR_SENDER_NAME_W
	"0x0065", // PR_SENT_REPRESENTING_EMAIL_ADDRESS_W
	"0x0042", // PR_SENT_REPRESENTING_NAME_W
}

func buildRestrictionXML(r FindRestriction) string {
	var conditions []string
	if from := strings.TrimSpace(r.From); from != "" {
		conditions = append(conditions, buildFromRestrictionXML(from))
	}
	if r.HasAttachment != nil {
		value := "false"
		if *r.HasAttachment {
			value = "true"
		}
		conditions = append(conditions, `<t:IsEqualTo><t:FieldURI FieldURI="item:HasAttachments" /><t:FieldURIOrConstant><t:Constant Value="`+value+`" /></t:FieldURIOrConstant></t:IsEqualTo>`)
	}
	if !r.After.IsZero() {
		conditions = append(conditions, `<t:IsGreaterThanOrEqualTo><t:FieldURI FieldURI="item:DateTimeReceived" /><t:FieldURIOrConstant><t:Constant Value="`+r.After.UTC().Format(time.RFC3339)+`" /></t:FieldURIOrConstant></t:IsGreaterThanOrEqualTo>`)
	}
	if !r.Before.IsZero() {
		conditions = append(conditions, `<t:IsLessThanOrEqualTo><t:FieldURI FieldURI="item:DateTimeReceived" /><t:FieldURIOrConstant><t:Constant Value="`+r.Before.UTC().Format(time.RFC3339)+`" /></t:FieldURIOrConstant></t:IsLessThanOrEqualTo>`)
	}
	if len(conditions) == 0 {
		return ""
	}
	if len(conditions) == 1 {
		return "\n      <m:Restriction>" + conditions[0] + "</m:Restriction>"
	}
	return "\n      <m:Restriction><t:And>" + strings.Join(conditions, "") + "</t:And></m:Restriction>"
}

// buildFromRestrictionXML emits an <t:Or> of substring matches against the
// sender's address/name extended properties so a needle like "tugraz" matches
// whether it appears in PR_SENDER_EMAIL_ADDRESS_W or PR_SENDER_NAME_W (and the
// representing variants for messages sent on behalf of someone else).
func buildFromRestrictionXML(needle string) string {
	escaped := xmlEscapeAttr(needle)
	var b strings.Builder
	b.WriteString(`<t:Or>`)
	for _, tag := range senderStringPropertyTags {
		b.WriteString(`<t:Contains ContainmentMode="Substring" ContainmentComparison="IgnoreCase"><t:ExtendedFieldURI PropertyTag="`)
		b.WriteString(tag)
		b.WriteString(`" PropertyType="String" /><t:Constant Value="`)
		b.WriteString(escaped)
		b.WriteString(`" /></t:Contains>`)
	}
	b.WriteString(`</t:Or>`)
	return b.String()
}
