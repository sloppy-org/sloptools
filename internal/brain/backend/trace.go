package backend

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

func logVisibleModelText(content string) {
	clean := strings.TrimSpace(sanitizeModelVisibleContent(content))
	if clean == "" {
		return
	}
	fmt.Fprintf(os.Stderr, "brain night: Scout visible text:\n%s\n", traceText(clean, 4000))
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
	if len(s) > capBytes {
		return s[:capBytes] + fmt.Sprintf("...[truncated %d bytes]", len(s)-capBytes)
	}
	return s
}

func describeToolCall(name string, args map[string]interface{}) string {
	switch name {
	case "web_search":
		return "Scout searches the web for: " + argString(args, "query")
	case "web_fetch":
		return "Scout reads a web page: " + argString(args, "url")
	case "helpy_zotero":
		query := nestedArgString(args, "query")
		if query == "" {
			return "Scout checks Zotero."
		}
		return "Scout checks Zotero for: " + query
	case "sloppy_brain":
		query := argString(args, "query")
		if query == "" {
			query = nestedArgString(args, "query")
		}
		if query == "" {
			return "Scout checks the brain vault."
		}
		return "Scout searches the brain vault for: " + query
	case "sloppy_contacts":
		return "Scout checks contacts for: " + firstNonEmpty(argString(args, "query"), nestedArgString(args, "query"))
	case "sloppy_calendar":
		return "Scout checks calendar evidence."
	case "sloppy_mail":
		return "Scout checks mail evidence for: " + firstNonEmpty(argString(args, "query"), nestedArgString(args, "query"))
	case "sloppy_source":
		return "Scout checks GitHub source evidence: " + firstNonEmpty(argString(args, "repo"), argString(args, "query"))
	case "helpy_tugonline":
		return "Scout checks TU Graz/TUGonline."
	case "helpy_tu4u":
		return "Scout checks TU4U."
	case "pdf_read":
		return "Scout reads a PDF: " + argString(args, "path")
	default:
		return "Scout uses " + sourceName(name) + "."
	}
}

func describeToolResult(name, result string) string {
	if strings.TrimSpace(result) == "" {
		return sourceName(name) + " returned no text."
	}
	if summary := summarizeJSONResult(name, result); summary != "" {
		return summary
	}
	return sourceName(name) + " returned: " + traceText(result, 900)
}

func summarizeJSONResult(name, result string) string {
	var obj map[string]interface{}
	if json.Unmarshal([]byte(result), &obj) != nil {
		return ""
	}
	if packets, ok := obj["packets"].([]interface{}); ok {
		return summarizeItems("Zotero found", packets)
	}
	if rawResult, ok := obj["result"].(map[string]interface{}); ok {
		if results, ok := rawResult["results"].([]interface{}); ok {
			return summarizeItems(sourceName(name)+" found", results)
		}
		if text, ok := rawResult["text"].(string); ok {
			return sourceName(name) + " returned text: " + traceText(text, 900)
		}
	}
	return ""
}

func summarizeItems(prefix string, items []interface{}) string {
	if len(items) == 0 {
		return prefix + " no matches."
	}
	var lines []string
	for i, raw := range items {
		if i >= 3 {
			break
		}
		item, _ := raw.(map[string]interface{})
		title, _ := item["title"].(string)
		url, _ := item["url"].(string)
		if title == "" {
			title, _ = item["citation"].(string)
		}
		if title == "" {
			title = fmt.Sprintf("match %d", i+1)
		}
		if url != "" {
			lines = append(lines, fmt.Sprintf("- %s (%s)", title, url))
		} else {
			lines = append(lines, "- "+title)
		}
	}
	return prefix + ":\n" + strings.Join(lines, "\n")
}

func sourceName(name string) string {
	switch name {
	case "web_search":
		return "web search"
	case "web_fetch":
		return "web page"
	case "helpy_zotero":
		return "Zotero"
	case "sloppy_brain":
		return "brain vault"
	case "sloppy_contacts":
		return "contacts"
	case "sloppy_calendar":
		return "calendar"
	case "sloppy_mail":
		return "mail"
	case "sloppy_source":
		return "GitHub source"
	case "helpy_tugonline":
		return "TUGonline"
	case "helpy_tu4u":
		return "TU4U"
	case "pdf_read":
		return "PDF reader"
	default:
		return name
	}
}

func quotaOperationName(name string) string {
	switch name {
	case "web_search":
		return "another web search"
	case "web_fetch":
		return "another page fetch"
	case "helpy_zotero":
		return "another Zotero lookup"
	case "sloppy_brain":
		return "another brain-vault search"
	case "sloppy_contacts":
		return "another contacts lookup"
	case "sloppy_calendar":
		return "another calendar lookup"
	case "sloppy_mail":
		return "another mail lookup"
	case "sloppy_source":
		return "another GitHub source lookup"
	case "pdf_read":
		return "another PDF read"
	default:
		return "another " + sourceName(name) + " call"
	}
}

func argString(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func nestedArgString(args map[string]interface{}, key string) string {
	if args == nil {
		return ""
	}
	nested, _ := args["args"].(map[string]interface{})
	return argString(nested, key)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
