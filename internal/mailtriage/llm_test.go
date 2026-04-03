package mailtriage

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestOpenAIClassifierParsesStructuredJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatalf("Decode() error: %v", err)
		}
		if got := strings.TrimSpace(r.URL.Path); got != "/v1/chat/completions" {
			t.Fatalf("path = %q, want /v1/chat/completions", got)
		}
		if payload["model"] != "qwen3.5-9b" {
			t.Fatalf("model = %#v, want qwen3.5-9b", payload["model"])
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"```json\\n{\\\"action\\\":\\\"archive\\\",\\\"archive_label\\\":\\\"simons24\\\",\\\"confidence\\\":0.96,\\\"reason\\\":\\\"project update\\\",\\\"signals\\\":[\\\"direct update\\\"]}\\n```\"}}]}"))
	}))
	defer server.Close()

	classifier := OpenAIClassifier{
		BaseURL: server.URL,
		Model:   "qwen3.5-9b",
	}
	decision, err := classifier.Classify(context.Background(), Message{
		ID:       "m1",
		Subject:  "Project update",
		Snippet:  "FYI",
		Body:     "Body",
		Provider: "exchange_ews",
	})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionArchive {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionArchive)
	}
	if decision.ArchiveLabel != "simons24" {
		t.Fatalf("ArchiveLabel = %q, want simons24", decision.ArchiveLabel)
	}
	if decision.Model != "qwen3.5-9b" {
		t.Fatalf("Model = %q, want qwen3.5-9b", decision.Model)
	}
}

func TestOpenAIClassifierParsesThinkingPreamble(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte("{\"choices\":[{\"message\":{\"content\":\"</think>\\n\\n{\\\"action\\\":\\\"cc\\\",\\\"confidence\\\":0.81,\\\"reason\\\":\\\"newsletter\\\",\\\"signals\\\":[\\\"fyi\\\"]}\"}}]}"))
	}))
	defer server.Close()

	classifier := OpenAIClassifier{
		BaseURL: server.URL,
		Model:   "qwen3.5-9b",
	}
	decision, err := classifier.Classify(context.Background(), Message{ID: "m2", Subject: "FYI"})
	if err != nil {
		t.Fatalf("Classify() error: %v", err)
	}
	if decision.Action != ActionCC {
		t.Fatalf("Action = %q, want %q", decision.Action, ActionCC)
	}
	if decision.Confidence != 0.81 {
		t.Fatalf("Confidence = %v, want 0.81", decision.Confidence)
	}
}

func TestOpenAIClassifierReturnsHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad gateway", http.StatusBadGateway)
	}))
	defer server.Close()

	classifier := OpenAIClassifier{BaseURL: server.URL}
	if _, err := classifier.Classify(context.Background(), Message{ID: "m1"}); err == nil {
		t.Fatal("Classify() error = nil, want non-nil")
	}
}

func TestBuildUserPromptIncludesFlagged(t *testing.T) {
	prompt := buildUserPrompt(Message{
		ID:        "m3",
		Subject:   "Important",
		IsRead:    true,
		IsFlagged: true,
	})
	if !strings.Contains(prompt, "Is flagged: true") {
		t.Fatalf("prompt missing flagged state: %q", prompt)
	}
}

func TestBuildUserPromptIncludesDistilledManualPolicy(t *testing.T) {
	prompt := buildUserPrompt(Message{
		ID:          "m4",
		Subject:     "Suspicious invite",
		ReviewCount: 37,
		PolicySummary: []string{
			"Semantics: trash reviewed from junk means confirmed junk/spam.",
			"Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.",
			"Folder rule: Junk-E-Mail usually -> trash (21/24 reviews)",
		},
		Examples: []Example{
			{
				Action:  "trash",
				Folder:  "Junk-E-Mail",
				Sender:  "spam@example.com",
				Subject: "Win a prize",
			},
		},
		LocalHints:     []string{"spam=0.91", "staleness=0.88"},
		ProtectedTopic: true,
		AgeDays:        42,
	})
	if !strings.Contains(prompt, "Manual review corpus size: 37") {
		t.Fatalf("prompt missing review corpus size: %q", prompt)
	}
	if !strings.Contains(prompt, "Treat the following distilled manual-review policy as authoritative mailbox-specific guidance:") {
		t.Fatalf("prompt missing distilled policy guidance: %q", prompt)
	}
	if !strings.Contains(prompt, "Distilled mailbox policy from manual reviews:") {
		t.Fatalf("prompt missing policy header: %q", prompt)
	}
	if !strings.Contains(prompt, "Semantics: trash reviewed from junk means confirmed junk/spam.") {
		t.Fatalf("prompt missing junk-trash semantics: %q", prompt)
	}
	if !strings.Contains(prompt, "Semantics: trash reviewed from inbox means discardable, but not necessarily spam/junk.") {
		t.Fatalf("prompt missing inbox-trash semantics: %q", prompt)
	}
	if !strings.Contains(prompt, "Folder rule: Junk-E-Mail usually -> trash (21/24 reviews)") {
		t.Fatalf("prompt missing policy line: %q", prompt)
	}
	if !strings.Contains(prompt, "Representative reviewed examples:") {
		t.Fatalf("prompt missing examples header: %q", prompt)
	}
	if !strings.Contains(prompt, "action=trash; folder=Junk-E-Mail; from=spam@example.com; subject=Win a prize") {
		t.Fatalf("prompt missing example detail: %q", prompt)
	}
	if !strings.Contains(prompt, "Local factor hints:") {
		t.Fatalf("prompt missing local factor hints: %q", prompt)
	}
	if !strings.Contains(prompt, "Protected topic: true") {
		t.Fatalf("prompt missing protected topic: %q", prompt)
	}
	if !strings.Contains(prompt, "Age days: 42") {
		t.Fatalf("prompt missing age days: %q", prompt)
	}
}

func TestDefaultSystemPromptSeparatesCCAndArchiveSemantics(t *testing.T) {
	prompt := DefaultSystemPrompt
	for _, snippet := range []string{
		"cc: not inbox-worthy; worth a skimmed read for information if the user has time, and no action is needed.",
		"archive: not inbox-worthy; keep only for later reference, with no skimmed read expected.",
		"Decide inbox vs cc by answering this first: does the email likely require action, follow-up, or deliberate attention from the user? If yes, prefer inbox.",
		"If no action is required from the user, prefer cc over inbox when the message is still worth a skim.",
		"For inbox vs cc, action-needed matters more than sender prestige or generic importance.",
		"Prefer cc instead of archive for newsletters, webinars, and FYI list traffic that is worth a skimmed read.",
		"Prefer archive instead of cc when the mail should be kept only as reference and does not merit a skimmed read.",
	} {
		if !strings.Contains(prompt, snippet) {
			t.Fatalf("DefaultSystemPrompt missing %q", snippet)
		}
	}
}
