package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func logVisibleModelText(round int, content string) {
	clean := strings.TrimSpace(stripXMLToolCalls(content))
	if clean == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "brain night: model visible round=%d preview=%s\n", round, traceText(clean, 700))
}

func traceJSON(v interface{}, capBytes int) string {
	if v == nil {
		return "{}"
	}
	body, err := json.Marshal(v)
	if err != nil {
		return traceText(fmt.Sprintf("%v", v), capBytes)
	}
	return traceText(string(body), capBytes)
}

func traceText(s string, capBytes int) string {
	s = strings.Join(strings.Fields(s), " ")
	if len(s) <= capBytes {
		return quoteTrace(s)
	}
	return quoteTrace(s[:capBytes] + fmt.Sprintf("...[truncated %d bytes]", len(s)-capBytes))
}

func quoteTrace(s string) string {
	body, _ := json.Marshal(s)
	return string(body)
}
