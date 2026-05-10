package backend

import (
	"encoding/json"
	"strings"
)

// qwenXMLCall holds one parsed <tool_call> block from qwen's native XML
// tool-calling format. Two sub-formats exist:
//
//	JSON:  <tool_call>\n{"name":"...","arguments":{...}}\n</tool_call>
//	Param: <tool_call>\n<function=name>\n<parameter=k>v</parameter>\n</function>\n</tool_call>
type qwenXMLCall struct {
	Name string
	Args map[string]interface{}
}

// parseQwenXMLCalls extracts all <tool_call> blocks from content.
// Returns nil when the content contains no such blocks.
func parseQwenXMLCalls(content string) []qwenXMLCall {
	if !strings.Contains(content, "<tool_call>") {
		return nil
	}
	var out []qwenXMLCall
	rest := content
	for {
		s := strings.Index(rest, "<tool_call>")
		if s < 0 {
			break
		}
		e := strings.Index(rest[s:], "</tool_call>")
		if e < 0 {
			break
		}
		inner := strings.TrimSpace(rest[s+len("<tool_call>") : s+e])
		rest = rest[s+e+len("</tool_call>"):]
		if tc := parseOneXMLCall(inner); tc != nil {
			out = append(out, *tc)
		}
	}
	return out
}

// parseOneXMLCall parses the content inside a single <tool_call> block.
func parseOneXMLCall(inner string) *qwenXMLCall {
	// JSON sub-format
	if strings.HasPrefix(inner, "{") {
		var obj map[string]interface{}
		if json.Unmarshal([]byte(inner), &obj) == nil {
			name, _ := obj["name"].(string)
			if name == "" {
				return nil
			}
			args, _ := obj["arguments"].(map[string]interface{})
			return &qwenXMLCall{Name: name, Args: args}
		}
	}
	// Parameter sub-format: <function=name>\n<parameter=k>v</parameter>...
	const fopen = "<function="
	fs := strings.Index(inner, fopen)
	if fs < 0 {
		return nil
	}
	fe := strings.Index(inner[fs+len(fopen):], ">")
	if fe < 0 {
		return nil
	}
	name := inner[fs+len(fopen) : fs+len(fopen)+fe]
	args := map[string]interface{}{}
	rest := inner[fs+len(fopen)+fe+1:]
	const popen = "<parameter="
	const pclose = "</parameter>"
	for {
		ps := strings.Index(rest, popen)
		if ps < 0 {
			break
		}
		pe := strings.Index(rest[ps+len(popen):], ">")
		if pe < 0 {
			break
		}
		key := rest[ps+len(popen) : ps+len(popen)+pe]
		after := rest[ps+len(popen)+pe+1:]
		ve := strings.Index(after, pclose)
		if ve < 0 {
			break
		}
		args[key] = after[:ve]
		rest = after[ve+len(pclose):]
	}
	return &qwenXMLCall{Name: name, Args: args}
}

// stripXMLToolCalls removes all <tool_call>...</tool_call> blocks from
// content. Used to check whether any non-tool text remains in a response
// and to sanitise the final output before returning it as the report.
func stripXMLToolCalls(content string) string {
	for strings.Contains(content, "<tool_call>") {
		s := strings.Index(content, "<tool_call>")
		e := strings.Index(content, "</tool_call>")
		if s < 0 || e < 0 || e < s {
			break
		}
		content = content[:s] + content[e+len("</tool_call>"):]
	}
	return strings.TrimSpace(content)
}
