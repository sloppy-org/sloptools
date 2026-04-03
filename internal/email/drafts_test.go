package email

import "testing"

func TestNormalizeDraftInputAllowsBlankDrafts(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		Subject: "  Draft subject  ",
		Body:    "draft body\r\n",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if normalized.Subject != "Draft subject" {
		t.Fatalf("subject = %q, want Draft subject", normalized.Subject)
	}
	if normalized.Body != "draft body" {
		t.Fatalf("body = %q, want draft body", normalized.Body)
	}
}

func TestNormalizeDraftSendInputRequiresRecipient(t *testing.T) {
	if _, err := NormalizeDraftSendInput(DraftInput{Subject: "Draft"}); err == nil {
		t.Fatal("NormalizeDraftSendInput() error = nil, want recipient validation")
	}
}

func TestNormalizeDraftAddressesNameWithAngleBrackets(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"Client <client@example.com>"},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "client@example.com" {
		t.Fatalf("To = %#v, want [client@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesNameWithComma(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{`"Smith, John" <john@example.com>`},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "john@example.com" {
		t.Fatalf("To = %#v, want [john@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesMultipleInOneString(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"alice@example.com, Bob <bob@example.com>"},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 2 {
		t.Fatalf("To = %#v, want 2 addresses", normalized.To)
	}
	if normalized.To[0] != "alice@example.com" || normalized.To[1] != "bob@example.com" {
		t.Fatalf("To = %#v, want [alice@example.com bob@example.com]", normalized.To)
	}
}

func TestNormalizeDraftAddressesDeduplicates(t *testing.T) {
	normalized, err := NormalizeDraftInput(DraftInput{
		To:      []string{"Alice <alice@example.com>", "alice@example.com"},
		Subject: "Test",
	})
	if err != nil {
		t.Fatalf("NormalizeDraftInput() error: %v", err)
	}
	if len(normalized.To) != 1 || normalized.To[0] != "alice@example.com" {
		t.Fatalf("To = %#v, want [alice@example.com]", normalized.To)
	}
}
