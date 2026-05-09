// Package meetings — quickprompt.go owns the surgical short-memo
// prompt (issue #56 E3.8). The watcher must spec the local OpenCode Qwen
// prompt for the quick-commitment path so the LLM emits exactly one
// outcome line with no commentary, and must reject responses that
// violate that contract.
package meetings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"unicode/utf8"
)

// QuickMemoSystemPrompt is the canonical instruction the watcher hands
// to the local OpenCode Qwen renderer for the short voice-memo path.
// The contract is intentionally narrow: emit one outcome line that
// captures the requested next action, no commentary, no markdown, no
// code fences, no enclosing quotes.
const QuickMemoSystemPrompt = `You convert a short voice-memo transcript into one GTD next action.
Reply with exactly one plain-text line that begins with an imperative verb
and names the requested outcome (who, what, when if mentioned).
Do not add commentary, headers, list markers, blank lines, code fences,
quotation marks, or trailing punctuation beyond a single period.
If the transcript states no actionable request, reply with the single
literal line: NO_ACTION.`

// QuickMemoTranscriptDelimiter separates the system prompt from the
// transcript in the stdin payload sent to the renderer. The literal
// fence makes the contract auditable in shell traces and lets a
// shell-script renderer split the input deterministically.
const QuickMemoTranscriptDelimiter = "\n---TRANSCRIPT---\n"

// MaxQuickOutcomeRunes caps the rendered outcome length so a misbehaving
// renderer cannot smuggle an entire paragraph past the one-line check.
// 240 runes covers a long but realistic GTD action and stays well under
// any sensible terminal/Markdown rendering width.
const MaxQuickOutcomeRunes = 240

// quickOutcomeNoAction is the sentinel the renderer emits when the
// transcript carried no actionable request. The pipeline treats it as a
// failure (no commitment is written) so the audio is preserved with a
// `.failed` sidecar that the user can review.
const quickOutcomeNoAction = "NO_ACTION"

// EnforceQuickOutcomeContract validates the renderer output against the
// QuickMemoSystemPrompt contract and returns the cleaned outcome line
// on success. NO_ACTION responses are reported as a typed error so the
// pipeline can distinguish "renderer worked but transcript was empty
// chatter" from "renderer crashed".
func EnforceQuickOutcomeContract(raw string) (string, error) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return "", errors.New("quick outcome empty")
	}
	if strings.Contains(trimmed, "\n") {
		return "", fmt.Errorf("quick outcome must be one line, got %q", trimmed)
	}
	if trimmed == quickOutcomeNoAction {
		return "", ErrQuickNoAction
	}
	if utf8.RuneCountInString(trimmed) > MaxQuickOutcomeRunes {
		return "", fmt.Errorf("quick outcome exceeds %d runes: %d", MaxQuickOutcomeRunes, utf8.RuneCountInString(trimmed))
	}
	if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "```") {
		return "", fmt.Errorf("quick outcome must be plain text, got %q", trimmed)
	}
	if strings.HasPrefix(trimmed, "- ") || strings.HasPrefix(trimmed, "* ") {
		return "", fmt.Errorf("quick outcome must not be a list item, got %q", trimmed)
	}
	if isQuoted(trimmed) {
		return "", fmt.Errorf("quick outcome must not be quoted, got %q", trimmed)
	}
	return trimmed, nil
}

// ErrQuickNoAction is returned by EnforceQuickOutcomeContract when the
// renderer reports the transcript carried no actionable request.
var ErrQuickNoAction = errors.New("quick renderer reported no action")

func isQuoted(value string) bool {
	if len(value) < 2 {
		return false
	}
	first, last := value[0], value[len(value)-1]
	return (first == '"' && last == '"') || (first == '\'' && last == '\'')
}

// OpencodeQuickRenderer wires the canonical QuickMemoSystemPrompt into a
// command-line LLM invocation (typically `opencode run` or a thin
// wrapper around `qwen`). The renderer feeds prompt + delimiter +
// transcript on stdin so the same binary can serve both the quick and
// long branches without per-mode wrappers, then enforces the one-line
// outcome contract.
func OpencodeQuickRenderer(command []string) QuickRenderer {
	return func(ctx context.Context, transcript string) (string, error) {
		if len(command) == 0 {
			return "", errors.New("quick renderer: command not configured")
		}
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Stdin = strings.NewReader(QuickMemoSystemPrompt + QuickMemoTranscriptDelimiter + transcript)
		cmd.Env = append(os.Environ(), "MEMO_KIND=quick")
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return "", fmt.Errorf("%s: %s", command[0], msg)
			}
			return "", err
		}
		return EnforceQuickOutcomeContract(stdout.String())
	}
}
