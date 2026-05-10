// Package sleep holds the deterministic classifier and the bulk → paid
// pipeline for the sleep-judge stage. Mirrors internal/brain/scout in
// shape so the same audit-sidecar / ledger-extras / cleanup contract
// applies to the editorial pass over a rendered sleep packet.
package sleep

import (
	"fmt"
	"strings"
	"unicode"
)

// PreflightPacketCap is the byte ceiling above which the bulk tier is
// skipped entirely. Originally 24 KB (calibrated for the qwen27b collapse
// at 167 KB on a ~40K-token window, #129). qwen3-MoE has a 256K-token
// context window (~1 MB of text), so the old cap was far too conservative
// and routed every real sleep packet straight to the paid tier. Raised to
// 200 KB (~50K tokens) — well within the model's window with room to spare
// for the system prompt and output tokens.
const PreflightPacketCap = 200 * 1024

// nonPrintableRatioThreshold is the fraction of body runes that may be
// non-printable + outside the basic ASCII / Latin-1 / common-Unicode
// printable range before the classifier flags the body as collapsed.
// 5% comfortably accommodates German umlauts and the occasional CJK
// name without false-positive while still catching the qwen
// "(g (g (g graphic" spam, which mixes parens, ASCII, and Mandarin
// near-ratios above 10%.
const nonPrintableRatioThreshold = 0.05

// trigramRepeatThreshold is the count above which a single 3-gram
// repeating in the body is treated as evidence of context-window
// collapse. 30 leaves room for legitimate reports that repeat a short
// pattern across many bullets while still tripping on the qwen spam
// fixture which repeats one trigram thousands of times.
const trigramRepeatThreshold = 30

// Decision is the deterministic classifier output for one bulk-tier
// sleep-judge call. Reason is empty when no escalation is needed.
type Decision struct {
	Escalate bool
	Reason   string
}

// classifySleepJudgeOutput reads a (possibly empty) bulk-tier judge
// body and the original packet size, and decides whether to route to
// the paid tier.
//
// Signals, in evaluation order:
//
//  1. packet size > PreflightPacketCap — pre-flight gate; route directly
//     to paid before bulk wastes wall-time.
//  2. opencode/llm-provider parse-error wrapper detected ("Failed to
//     parse input", leaked "<think>" tag).
//  3. non-printable + control-byte ratio above 5%.
//  4. any 3-gram repeats more than `trigramRepeatThreshold` times.
//
// Caller passes the original packet size (in bytes) so the pre-flight
// signal works without re-reading the packet at classification time.
func classifySleepJudgeOutput(body string, packetSize int) Decision {
	if packetSize > PreflightPacketCap {
		return Decision{Escalate: true, Reason: fmt.Sprintf("packet size %d > %d", packetSize, PreflightPacketCap)}
	}
	if reason := scanParseErrorWrapper(body); reason != "" {
		return Decision{Escalate: true, Reason: reason}
	}
	if ratio, over := nonPrintableRatio(body); over {
		return Decision{Escalate: true, Reason: fmt.Sprintf("non-printable ratio %.2f exceeds %.2f", ratio, nonPrintableRatioThreshold)}
	}
	if hits, over := topTrigramRepeats(body); over {
		return Decision{Escalate: true, Reason: fmt.Sprintf("trigram repetition %d exceeds %d", hits, trigramRepeatThreshold)}
	}
	return Decision{}
}

// scanParseErrorWrapper detects the opencode / llm-provider parse-error
// wrapper bodies. Empty return means clean.
func scanParseErrorWrapper(body string) string {
	trimmed := strings.TrimSpace(body)
	lower := strings.ToLower(trimmed)
	if strings.HasPrefix(lower, "failed to parse input") {
		return "opencode parse-error wrapper"
	}
	if strings.HasPrefix(lower, "<think>") {
		return "leaked <think> tag in body"
	}
	if strings.Contains(lower, "<think>\n") || strings.Contains(lower, "<think>\r\n") {
		return "leaked <think> tag in body"
	}
	return ""
}

// nonPrintableRatio counts runes that are non-printable or sit outside
// the printable categories (Letter / Number / Punctuation / Symbol /
// Space). Returns the ratio and whether it exceeds the threshold.
// Ignores ordinary whitespace (\n, \t, space) so well-formed reports
// with newlines do not skew the count.
func nonPrintableRatio(body string) (float64, bool) {
	if body == "" {
		return 0, false
	}
	total := 0
	bad := 0
	for _, r := range body {
		total++
		if r == '\n' || r == '\r' || r == '\t' || r == ' ' {
			continue
		}
		if unicode.IsLetter(r) || unicode.IsDigit(r) || unicode.IsPunct(r) || unicode.IsSymbol(r) {
			continue
		}
		bad++
	}
	if total == 0 {
		return 0, false
	}
	ratio := float64(bad) / float64(total)
	return ratio, ratio > nonPrintableRatioThreshold
}

// topTrigramRepeats counts the most-repeated 3-rune window. Returns the
// hit count for the worst trigram and whether it exceeds the
// `trigramRepeatThreshold`. Trigrams that span only whitespace are
// skipped so legitimate bullet lists do not register.
func topTrigramRepeats(body string) (int, bool) {
	runes := []rune(body)
	if len(runes) < 3 {
		return 0, false
	}
	counts := make(map[string]int, len(runes))
	worst := 0
	for i := 0; i+3 <= len(runes); i++ {
		w := runes[i : i+3]
		if isAllWhitespace(w) {
			continue
		}
		key := string(w)
		counts[key]++
		if counts[key] > worst {
			worst = counts[key]
		}
	}
	return worst, worst > trigramRepeatThreshold
}

func isAllWhitespace(rs []rune) bool {
	for _, r := range rs {
		if !unicode.IsSpace(r) {
			return false
		}
	}
	return true
}
