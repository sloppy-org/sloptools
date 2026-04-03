package mailtriage

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

const DefaultSystemPrompt = `You classify emails for a personal triage system.

Return strict JSON with shape:
{"action":"inbox|cc|archive|trash","archive_label":"optional short label","confidence":0.0,"reason":"short reason","signals":["short signal"]}

Semantics:
- inbox: the user should look at this or act on it.
- cc: not inbox-worthy; worth a skimmed read for information if the user has time, and no action is needed.
- archive: not inbox-worthy; keep only for later reference, with no skimmed read expected.
- trash: clearly useless, spam, or safe to discard.

Rules:
- Decide inbox vs cc by answering this first: does the email likely require action, follow-up, or deliberate attention from the user? If yes, prefer inbox.
- If no action is required from the user, prefer cc over inbox when the message is still worth a skim.
- When unsure between inbox and anything else, choose inbox.
- When unsure between archive and trash, choose archive.
- Use archive_label only for clear project/reference buckets.
- Do not use "already read" by itself as a reason to archive or trash.
- If the message is flagged, treat that as a strong inbox signal.
- Treat folder-aware manual-review policy as authoritative when it is provided.
- In particular: trash reviewed from junk means confirmed spam/junk; trash reviewed from inbox means discardable but not necessarily spam.
- Prefer inbox for direct human mail from collaborators, admins, or teaching/research contacts when action or attention may still be needed.
- For inbox vs cc, action-needed matters more than sender prestige or generic importance.
- Prefer cc instead of archive for newsletters, webinars, and FYI list traffic that is worth a skimmed read.
- Prefer archive instead of cc when the mail should be kept only as reference and does not merit a skimmed read.
- If a message is already in junk/spam but is still research-adjacent (for example journals, conferences, plasma physics, acoustics, machine learning, physics), prefer archive over trash unless it is obviously scammy.
- Compare the message against the distilled manual policy before choosing an action.
- Treat any provided local factor hints as probabilistic evidence, not as absolute truth.
- Confidence is 0.0 to 1.0.
- Keep reason and signals short.`

const defaultRequestTimeout = 20 * time.Second

type OpenAIClassifier struct {
	BaseURL      string
	Model        string
	SystemPrompt string
	HTTPClient   *http.Client
	Timeout      time.Duration
}

type chatCompletionResponse struct {
	Choices []struct {
		Message struct {
			Content string `json:"content"`
		} `json:"message"`
	} `json:"choices"`
}

func (c OpenAIClassifier) Classify(ctx context.Context, message Message) (Decision, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(c.BaseURL), "/")
	if baseURL == "" {
		return Decision{}, fmt.Errorf("mail triage classifier base URL is required")
	}
	model := strings.TrimSpace(c.Model)
	if model == "" {
		model = "local"
	}
	systemPrompt := strings.TrimSpace(c.SystemPrompt)
	if systemPrompt == "" {
		systemPrompt = DefaultSystemPrompt
	}
	body, _ := json.Marshal(map[string]any{
		"model":       model,
		"temperature": 0,
		"max_tokens":  256,
		"response_format": map[string]any{
			"type": "json_object",
		},
		"chat_template_kwargs": map[string]any{
			"enable_thinking": false,
		},
		"messages": []map[string]string{
			{"role": "system", "content": systemPrompt},
			{"role": "user", "content": buildUserPrompt(message)},
		},
	})
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = defaultRequestTimeout
	}
	requestCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(requestCtx, http.MethodPost, baseURL+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Decision{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return Decision{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		return Decision{}, fmt.Errorf("mail triage classifier HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	var payload chatCompletionResponse
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&payload); err != nil {
		return Decision{}, err
	}
	if len(payload.Choices) == 0 {
		return Decision{}, nil
	}
	content := strings.TrimSpace(stripThinkingPreamble(stripCodeFence(payload.Choices[0].Message.Content)))
	if content == "" {
		return Decision{}, nil
	}
	var decision Decision
	if err := json.Unmarshal([]byte(content), &decision); err != nil {
		return Decision{}, err
	}
	decision.Model = model
	return normalizeDecision(decision), nil
}

func buildUserPrompt(message Message) string {
	var b strings.Builder
	fmt.Fprintf(&b, "Message ID: %s\n", strings.TrimSpace(message.ID))
	if value := strings.TrimSpace(message.Provider); value != "" {
		fmt.Fprintf(&b, "Provider: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountLabel); value != "" {
		fmt.Fprintf(&b, "Account: %s\n", value)
	}
	if value := strings.TrimSpace(message.AccountAddress); value != "" {
		fmt.Fprintf(&b, "Account address: %s\n", value)
	}
	if value := strings.TrimSpace(message.Sender); value != "" {
		fmt.Fprintf(&b, "From: %s\n", value)
	}
	if len(message.Recipients) > 0 {
		fmt.Fprintf(&b, "Recipients: %s\n", strings.Join(message.Recipients, ", "))
	}
	if value := strings.TrimSpace(message.Subject); value != "" {
		fmt.Fprintf(&b, "Subject: %s\n", value)
	}
	if value := strings.TrimSpace(message.Snippet); value != "" {
		fmt.Fprintf(&b, "Snippet: %s\n", value)
	}
	if value := strings.TrimSpace(message.Body); value != "" {
		body := value
		if len(body) > 6000 {
			body = body[:6000]
		}
		fmt.Fprintf(&b, "Body:\n%s\n", body)
	}
	if len(message.Labels) > 0 {
		fmt.Fprintf(&b, "Labels: %s\n", strings.Join(message.Labels, ", "))
	}
	fmt.Fprintf(&b, "Has attachments: %t\n", message.HasAttachments)
	fmt.Fprintf(&b, "Is read: %t\n", message.IsRead)
	fmt.Fprintf(&b, "Is flagged: %t\n", message.IsFlagged)
	if message.ReviewCount > 0 {
		fmt.Fprintf(&b, "Manual review corpus size: %d\n", message.ReviewCount)
	}
	if len(message.PolicySummary) > 0 {
		fmt.Fprintf(&b, "Treat the following distilled manual-review policy as authoritative mailbox-specific guidance:\n")
		fmt.Fprintf(&b, "Distilled mailbox policy from manual reviews:\n")
		for _, line := range message.PolicySummary {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
		}
	}
	if len(message.LocalHints) > 0 {
		fmt.Fprintf(&b, "Local factor hints:\n")
		for _, line := range message.LocalHints {
			fmt.Fprintf(&b, "- %s\n", strings.TrimSpace(line))
		}
	}
	if message.ProtectedTopic {
		fmt.Fprintf(&b, "Protected topic: true\n")
	}
	if message.AgeDays > 0 {
		fmt.Fprintf(&b, "Age days: %d\n", message.AgeDays)
	}
	if len(message.Examples) > 0 {
		fmt.Fprintf(&b, "Representative reviewed examples:\n")
		for i, example := range message.Examples {
			if i >= maxTrainingExamples {
				break
			}
			fmt.Fprintf(
				&b,
				"- action=%s; folder=%s; from=%s; subject=%s\n",
				strings.TrimSpace(example.Action),
				strings.TrimSpace(example.Folder),
				strings.TrimSpace(example.Sender),
				strings.TrimSpace(example.Subject),
			)
		}
	}
	return strings.TrimSpace(b.String())
}

func stripCodeFence(raw string) string {
	clean := strings.TrimSpace(raw)
	if !strings.HasPrefix(clean, "```") {
		return clean
	}
	lines := strings.Split(clean, "\n")
	if len(lines) == 0 {
		return clean
	}
	lines = lines[1:]
	for len(lines) > 0 && strings.TrimSpace(lines[len(lines)-1]) == "```" {
		lines = lines[:len(lines)-1]
	}
	return strings.TrimSpace(strings.Join(lines, "\n"))
}

func stripThinkingPreamble(raw string) string {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return clean
	}
	if strings.HasPrefix(clean, "<think>") {
		if idx := strings.Index(clean, "</think>"); idx >= 0 {
			clean = clean[idx+len("</think>"):]
		}
	}
	if strings.HasPrefix(clean, "</think>") {
		clean = clean[len("</think>"):]
	}
	return strings.TrimSpace(clean)
}
