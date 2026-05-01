package meetings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestEnforceQuickOutcomeContractAcceptsSingleLine(t *testing.T) {
	got, err := EnforceQuickOutcomeContract("  Send Ada the budget by Friday.\n")
	if err != nil {
		t.Fatalf("plain one-liner must pass: %v", err)
	}
	if got != "Send Ada the budget by Friday." {
		t.Fatalf("trim and return cleaned outcome, got %q", got)
	}
}

func TestEnforceQuickOutcomeContractRejectsMultiLine(t *testing.T) {
	_, err := EnforceQuickOutcomeContract("Send Ada the budget by Friday.\nAlso CC Ben.")
	if err == nil || !strings.Contains(err.Error(), "one line") {
		t.Fatalf("multi-line response must be rejected, got %v", err)
	}
}

func TestEnforceQuickOutcomeContractRejectsEmpty(t *testing.T) {
	_, err := EnforceQuickOutcomeContract("   \n  ")
	if err == nil || !strings.Contains(err.Error(), "empty") {
		t.Fatalf("blank response must be rejected, got %v", err)
	}
}

func TestEnforceQuickOutcomeContractRejectsMarkdownAndQuotes(t *testing.T) {
	cases := []string{
		"# Outcome: send the budget",
		"```send the budget```",
		"- send the budget",
		"\"send the budget\"",
	}
	for _, raw := range cases {
		if _, err := EnforceQuickOutcomeContract(raw); err == nil {
			t.Fatalf("must reject decorated outcome %q", raw)
		}
	}
}

func TestEnforceQuickOutcomeContractRejectsOversize(t *testing.T) {
	long := strings.Repeat("a", MaxQuickOutcomeRunes+1)
	if _, err := EnforceQuickOutcomeContract(long); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("oversize outcome must be rejected, got %v", err)
	}
}

func TestEnforceQuickOutcomeContractReportsNoAction(t *testing.T) {
	_, err := EnforceQuickOutcomeContract(quickOutcomeNoAction)
	if !errors.Is(err, ErrQuickNoAction) {
		t.Fatalf("NO_ACTION sentinel must surface ErrQuickNoAction, got %v", err)
	}
}

func TestOpencodeQuickRendererPassesPromptOnStdin(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script renderer only exercised on POSIX")
	}
	dir := t.TempDir()
	stdinSink := filepath.Join(dir, "stdin.bin")
	script := filepath.Join(dir, "render.sh")
	body := "#!/bin/sh\ncat > " + stdinSink + "\necho 'Send Ada the budget by Friday.'\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	render := OpencodeQuickRenderer([]string{script})
	out, err := render(context.Background(), "Remember to mail the budget to Ada by Friday.")
	if err != nil {
		t.Fatalf("renderer: %v", err)
	}
	if out != "Send Ada the budget by Friday." {
		t.Fatalf("renderer must enforce contract on stdout, got %q", out)
	}
	captured, err := os.ReadFile(stdinSink)
	if err != nil {
		t.Fatalf("read stdin sink: %v", err)
	}
	payload := string(captured)
	if !strings.HasPrefix(payload, QuickMemoSystemPrompt) {
		t.Fatalf("stdin must start with QuickMemoSystemPrompt, got %q", payload)
	}
	if !strings.Contains(payload, QuickMemoTranscriptDelimiter) {
		t.Fatalf("stdin must include transcript delimiter, got %q", payload)
	}
	if !strings.HasSuffix(payload, "Remember to mail the budget to Ada by Friday.") {
		t.Fatalf("stdin must end with the transcript, got %q", payload)
	}
}

func TestOpencodeQuickRendererRejectsMultiLineFromCommand(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("shell-script renderer only exercised on POSIX")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "render.sh")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf 'first\\nsecond\\n'\n"), 0o755); err != nil {
		t.Fatalf("write script: %v", err)
	}
	render := OpencodeQuickRenderer([]string{script})
	if _, err := render(context.Background(), "anything"); err == nil || !strings.Contains(err.Error(), "one line") {
		t.Fatalf("multi-line stdout must surface contract failure, got %v", err)
	}
}
