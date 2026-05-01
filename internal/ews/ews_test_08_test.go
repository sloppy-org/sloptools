package ews

import (
	"strings"
	"testing"
	"time"
)

func TestBuildRestrictionXMLFromOnlyUsesExtendedProperties(t *testing.T) {
	xml := buildRestrictionXML(FindRestriction{From: "albert@tugraz.at"})
	if xml == "" {
		t.Fatalf("expected restriction XML, got empty")
	}
	for _, want := range []string{
		"<m:Restriction>",
		"<t:Or>",
		`PropertyTag="0x0C1F"`,
		`PropertyTag="0x0C1A"`,
		`PropertyTag="0x0065"`,
		`PropertyTag="0x0042"`,
		`PropertyType="String"`,
		`ContainmentMode="Substring"`,
		`ContainmentComparison="IgnoreCase"`,
		`<t:Constant Value="albert@tugraz.at" />`,
	} {
		if !strings.Contains(xml, want) {
			t.Fatalf("expected %q in restriction XML, got %s", want, xml)
		}
	}
	for _, banned := range []string{
		`FieldURI="message:From"`,
		"<t:And>",
	} {
		if strings.Contains(xml, banned) {
			t.Fatalf("did not expect %q in restriction XML, got %s", banned, xml)
		}
	}
	if got := strings.Count(xml, "<t:Contains "); got != len(senderStringPropertyTags) {
		t.Fatalf("expected %d <t:Contains> clauses, got %d in %s", len(senderStringPropertyTags), got, xml)
	}
}

func TestBuildRestrictionXMLFromEscapesValue(t *testing.T) {
	xml := buildRestrictionXML(FindRestriction{From: `a"&<b>`})
	want := `<t:Constant Value="` + xmlEscapeAttr(`a"&<b>`) + `" />`
	if !strings.Contains(xml, want) {
		t.Fatalf("expected escaped sender value %q, got %s", want, xml)
	}
	for _, raw := range []string{`Value="a"`, "Value=\"a&<b>"} {
		if strings.Contains(xml, raw) {
			t.Fatalf("expected escaping to neutralize %q, got %s", raw, xml)
		}
	}
}

func TestBuildRestrictionXMLFromAndHasAttachmentWraps(t *testing.T) {
	yes := true
	xml := buildRestrictionXML(FindRestriction{From: "tugraz", HasAttachment: &yes})
	if !strings.HasPrefix(xml, "\n      <m:Restriction><t:And>") {
		t.Fatalf("expected <t:And> outer wrapper, got %s", xml)
	}
	if !strings.Contains(xml, "<t:Or>") {
		t.Fatalf("expected <t:Or> group for sender clauses, got %s", xml)
	}
	if !strings.Contains(xml, `<t:IsEqualTo><t:FieldURI FieldURI="item:HasAttachments" />`) {
		t.Fatalf("expected HasAttachments clause, got %s", xml)
	}
	orStart := strings.Index(xml, "<t:Or>")
	orEnd := strings.Index(xml, "</t:Or>")
	if orStart < 0 || orEnd < 0 || orEnd < orStart {
		t.Fatalf("malformed <t:Or> placement: %s", xml)
	}
	if strings.Contains(xml[orStart:orEnd], "IsEqualTo") {
		t.Fatalf("HasAttachments must sit beside the <t:Or>, not inside it: %s", xml)
	}
}

func TestBuildRestrictionXMLFromWithDateRange(t *testing.T) {
	after := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	before := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	xml := buildRestrictionXML(FindRestriction{From: "tugraz", After: after, Before: before})
	if !strings.Contains(xml, "<t:And>") {
		t.Fatalf("expected <t:And> wrapper for combined restriction, got %s", xml)
	}
	if !strings.Contains(xml, "<t:Or>") {
		t.Fatalf("expected <t:Or> sender group, got %s", xml)
	}
	if !strings.Contains(xml, `<t:Constant Value="2026-01-01T00:00:00Z" />`) {
		t.Fatalf("expected RFC3339 After bound, got %s", xml)
	}
	if !strings.Contains(xml, `<t:Constant Value="2026-05-01T00:00:00Z" />`) {
		t.Fatalf("expected RFC3339 Before bound, got %s", xml)
	}
}

func TestBuildRestrictionXMLWhitespaceFromIsEmpty(t *testing.T) {
	if got := buildRestrictionXML(FindRestriction{From: "   "}); got != "" {
		t.Fatalf("expected empty restriction for whitespace-only From, got %s", got)
	}
}

func TestBuildRestrictionXMLNoFromOmitsOr(t *testing.T) {
	yes := true
	xml := buildRestrictionXML(FindRestriction{HasAttachment: &yes})
	if strings.Contains(xml, "<t:Or>") {
		t.Fatalf("did not expect <t:Or> when From is empty, got %s", xml)
	}
	if !strings.Contains(xml, `FieldURI="item:HasAttachments"`) {
		t.Fatalf("expected HasAttachments clause, got %s", xml)
	}
}
