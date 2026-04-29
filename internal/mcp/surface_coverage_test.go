package mcp

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/sloppy-org/sloptools/internal/surface"
)

// TestGroupwareDocListsEveryMCPTool ensures the groupware doc inventory
// matches the code inventory. Tools in the Canvas/Handoff/Temp/Workspace/
// Items/Artifacts/Actors sections are out of scope for the groupware doc.
func TestGroupwareDocListsEveryMCPTool(t *testing.T) {
	docPath := "../../docs/groupware.md"
	data, err := os.ReadFile(docPath)
	if err != nil {
		t.Fatalf("read groupware doc: %v", err)
	}
	doc := string(data)

	// Extract tool names from the doc. The doc uses backtick-quoted names
	// like `mail_send`, `calendar_events`, etc. Only match names that
	// look like actual MCP tools (mail/contact/calendar/task/evernote prefix), not
	// parameter references like `task_id` or `contact_id`.
	docRe := regexp.MustCompile("`((?:mail|contact|calendar|task|evernote)_[a-z][a-z0-9_]*)`")
	docNames := make(map[string]bool)
	for _, m := range docRe.FindAllStringSubmatch(doc, -1) {
		docNames[m[1]] = true
	}

	// Build the set of groupware tool names from surface.MCPTools.
	// Out-of-scope prefixes (Canvas, Handoff, Temp, Workspace, Items,
	// Artifacts, Actors) are excluded.
	groupwarePrefixes := []string{
		"canvas_",
		"handoff.",
		"temp_file_",
		"workspace_",
		"item_",
		"artifact_",
		"actor_",
		"brain.",
		"brain_",
	}

	codeNames := make(map[string]bool)
	for _, tool := range surface.MCPTools {
		skip := false
		for _, prefix := range groupwarePrefixes {
			if strings.HasPrefix(tool.Name, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			codeNames[tool.Name] = true
		}
	}

	// Check that every code name appears in the doc.
	for name := range codeNames {
		if !docNames[name] {
			t.Errorf("tool %q in code but missing from doc", name)
		}
	}

	// Check that every doc name (that looks like a groupware tool) is in code.
	knownParams := map[string]bool{"account_id": true, "task_id": true, "contact_id": true, "calendar_id": true, "message_id": true, "event_id": true, "list_id": true, "filter_id": true, "draft_id": true, "attachment_id": true, "session_id": true, "handoff_id": true, "workspace_id": true, "artifact_id": true, "actor_id": true, "item_id": true, "source_account_id": true, "target_account_id": true, "target_folder": true}
	for name := range docNames {
		if knownParams[name] {
			continue
		}
		if !codeNames[name] {
			t.Errorf("name %q in doc but not a known tool", name)
		}
	}
}

func TestTaskMutationSurfaceExposesSourceMetadata(t *testing.T) {
	for _, name := range []string{"task_create", "task_update"} {
		tool := surfaceToolByName(t, name)
		for _, prop := range []string{"start_at", "follow_up_at", "deadline", "description", "section_id", "parent_id", "labels", "assignee_id"} {
			if _, ok := tool.Properties[prop]; !ok {
				t.Fatalf("%s missing property %s", name, prop)
			}
		}
	}
}

func surfaceToolByName(t *testing.T, name string) surface.Tool {
	t.Helper()
	for _, tool := range surface.MCPTools {
		if tool.Name == name {
			return tool
		}
	}
	t.Fatalf("surface tool %q not found", name)
	return surface.Tool{}
}
