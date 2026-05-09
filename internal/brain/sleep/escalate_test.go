package sleep

import (
	"strings"
	"testing"
)

// classifySleepJudgeOutput is the deterministic gate that decides whether
// the bulk-tier local OpenCode Qwen editorial pass on a sleep packet is
// trustworthy or whether the harness must throw the same packet at the
// paid tier (codex). #129 lists four signals; each gets a dedicated
// table-driven case so a regression on one signal does not silently
// disable the others.

func TestClassifySleepJudge_PreflightPacketSizeGate(t *testing.T) {
	body := "# Sleep report\n\n- normal\n"
	// The classifier is also called pre-flight with empty body and the
	// real packet size. A 167 KB packet (the size that crashed tonight)
	// must route directly to paid before bulk wastes wall-time.
	d := classifySleepJudgeOutput(body, 167*1024)
	if !d.Escalate {
		t.Fatalf("packet size 167 KB must route directly to paid: %+v", d)
	}
	if !strings.Contains(d.Reason, "packet size") {
		t.Fatalf("reason should name packet size: %q", d.Reason)
	}
}

func TestClassifySleepJudge_PacketUnderGate_NoEscalation(t *testing.T) {
	body := "# Sleep report\n\n- normal\n## Section\n- ok\n"
	d := classifySleepJudgeOutput(body, 8*1024)
	if d.Escalate {
		t.Fatalf("clean output under gate must not escalate: %+v", d)
	}
}

func TestClassifySleepJudge_OpencodeParseErrorWrapper_Escalates(t *testing.T) {
	// llm-provider parse-error wrapper observed when qwen's structured
	// output collapses; the wrapper text leaks through into the body.
	body := "Failed to parse input: unexpected EOF in JSON\n"
	d := classifySleepJudgeOutput(body, 4*1024)
	if !d.Escalate {
		t.Fatalf("'Failed to parse input' wrapper must escalate: %+v", d)
	}
	if !strings.Contains(d.Reason, "parse") {
		t.Fatalf("reason should name parse error: %q", d.Reason)
	}
}

func TestClassifySleepJudge_LeakedThinkingTag_Escalates(t *testing.T) {
	body := "<think>\nlet me reason about this\n</think>\n# Sleep report\n- ok\n"
	d := classifySleepJudgeOutput(body, 4*1024)
	if !d.Escalate {
		t.Fatalf("leaked <think> tag must escalate: %+v", d)
	}
	if !strings.Contains(d.Reason, "think") {
		t.Fatalf("reason should mention think tag: %q", d.Reason)
	}
}

func TestClassifySleepJudge_NonPrintableRatioOverThreshold_Escalates(t *testing.T) {
	// Build a body where >5% of runes are non-printable / non-ASCII.
	// 200 ASCII chars + 30 high-bit / control chars = 30/230 ~= 13%.
	body := strings.Repeat("a", 200) + strings.Repeat("\x01一", 15)
	d := classifySleepJudgeOutput(body, 4*1024)
	if !d.Escalate {
		t.Fatalf("non-printable ratio over 5%% must escalate: %+v", d)
	}
	if !strings.Contains(d.Reason, "non-printable") {
		t.Fatalf("reason should name non-printable ratio: %q", d.Reason)
	}
}

func TestClassifySleepJudge_NormalNonAscii_NoEscalation(t *testing.T) {
	// Plain prose with German umlauts and one Chinese name should NOT
	// trip the non-printable gate. ASCII-bytes ratio matters less than
	// the runtime non-printable count.
	body := "# Bericht\n\nÜberprüfung der Daten für Müller, Schöberl und 王力宏.\n" +
		"- Notiz: Übergabe der Lageberichte. Ärger über das Datum.\n"
	d := classifySleepJudgeOutput(body, 4*1024)
	if d.Escalate {
		t.Fatalf("normal multilingual prose must not escalate: %+v", d)
	}
}

func TestClassifySleepJudge_RepeatingTrigram_Escalates(t *testing.T) {
	// The 167 KB qwen collapse showed the trigram "g g g" repeating
	// thousands of times. Threshold is 30 repeats of any one trigram.
	body := strings.Repeat("(g (g (g graphic graphic Mar Mar ", 35)
	d := classifySleepJudgeOutput(body, 4*1024)
	if !d.Escalate {
		t.Fatalf("trigram repeating >30 times must escalate: %+v", d)
	}
	if !strings.Contains(d.Reason, "repetition") && !strings.Contains(d.Reason, "trigram") {
		t.Fatalf("reason should name repetition: %q", d.Reason)
	}
}

func TestClassifySleepJudge_LegitimateRepeatedBullet_NoEscalation(t *testing.T) {
	// A normal report that happens to repeat a few bullets should not
	// escalate. The trigram threshold is high enough (30) that ordinary
	// reports stay under it.
	var b strings.Builder
	b.WriteString("# Sleep report\n\n## Verified\n")
	for i := 0; i < 5; i++ {
		b.WriteString("- entity: confirmed (source: vault)\n")
	}
	b.WriteString("\n## Suggestions\n- compress textbook prose in five folder notes\n")
	d := classifySleepJudgeOutput(b.String(), 4*1024)
	if d.Escalate {
		t.Fatalf("legitimate repeated bullets must not escalate: %+v", d)
	}
}
