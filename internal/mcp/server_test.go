package mcp

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestToolDefinitionsEmitsProperties(t *testing.T) {
	defs := toolDefinitions()
	var tempCreateDef map[string]interface{}
	var tempRemoveDef map[string]interface{}
	var calendarEventsDef map[string]interface{}
	var calendarCreateDef map[string]interface{}
	var mailActionDef map[string]interface{}
	for _, d := range defs {
		switch d["name"] {
		case "temp_file_create":
			tempCreateDef = d
		case "temp_file_remove":
			tempRemoveDef = d
		case "calendar_events":
			calendarEventsDef = d
		case "calendar_event_create":
			calendarCreateDef = d
		case "mail_action":
			mailActionDef = d
		}
	}
	if tempCreateDef == nil {
		t.Fatal("temp_file_create not found in tool definitions")
	}
	if tempRemoveDef == nil {
		t.Fatal("temp_file_remove not found in tool definitions")
	}
	if calendarEventsDef == nil {
		t.Fatal("calendar_events not found in tool definitions")
	}
	if calendarCreateDef == nil {
		t.Fatal("calendar_event_create not found in tool definitions")
	}
	if mailActionDef == nil {
		t.Fatal("mail_action not found in tool definitions")
	}
	tempCreateSchema, _ := tempCreateDef["inputSchema"].(map[string]interface{})
	tempCreateProps, _ := tempCreateSchema["properties"].(map[string]interface{})
	if tempCreateProps["prefix"] == nil || tempCreateProps["suffix"] == nil || tempCreateProps["content"] == nil {
		t.Fatalf("temp_file_create missing expected properties: %#v", tempCreateProps)
	}
	tempRemoveSchema, _ := tempRemoveDef["inputSchema"].(map[string]interface{})
	tempRemoveProps, _ := tempRemoveSchema["properties"].(map[string]interface{})
	if tempRemoveProps["path"] == nil {
		t.Fatalf("temp_file_remove missing path property: %#v", tempRemoveProps)
	}
	calendarSchema, _ := calendarEventsDef["inputSchema"].(map[string]interface{})
	calendarProps, _ := calendarSchema["properties"].(map[string]interface{})
	if calendarProps["calendar_id"] == nil || calendarProps["days"] == nil || calendarProps["limit"] == nil || calendarProps["query"] == nil {
		t.Fatalf("calendar_events missing expected properties: %#v", calendarProps)
	}
	calendarCreateSchema, _ := calendarCreateDef["inputSchema"].(map[string]interface{})
	calendarCreateProps, _ := calendarCreateSchema["properties"].(map[string]interface{})
	if calendarCreateProps["summary"] == nil || calendarCreateProps["start"] == nil || calendarCreateProps["duration_minutes"] == nil || calendarCreateProps["all_day"] == nil {
		t.Fatalf("calendar_event_create missing expected properties: %#v", calendarCreateProps)
	}
	mailActionSchema, _ := mailActionDef["inputSchema"].(map[string]interface{})
	mailActionProps, _ := mailActionSchema["properties"].(map[string]interface{})
	if mailActionProps["until"] == nil {
		t.Fatalf("mail_action missing until property: %#v", mailActionProps)
	}
	actionProp, _ := mailActionProps["action"].(map[string]interface{})
	actionEnum, _ := actionProp["enum"].([]string)
	if len(actionEnum) == 0 {
		rawEnum, _ := actionProp["enum"].([]interface{})
		actionEnum = make([]string, 0, len(rawEnum))
		for _, value := range rawEnum {
			if text, ok := value.(string); ok {
				actionEnum = append(actionEnum, text)
			}
		}
	}
	foundDefer := false
	for _, value := range actionEnum {
		if value == "defer" {
			foundDefer = true
			break
		}
	}
	if !foundDefer {
		t.Fatalf("mail_action enum missing defer: %#v", actionProp["enum"])
	}
}

func TestTempFileCreateAndRemove(t *testing.T) {
	projectDir := t.TempDir()
	s := NewServer(projectDir)
	created, err := s.callTool("temp_file_create", map[string]interface{}{
		"prefix":  "spec",
		"suffix":  ".md",
		"content": "# temp",
	})
	if err != nil {
		t.Fatalf("temp_file_create failed: %v", err)
	}
	path, _ := created["path"].(string)
	if !strings.HasPrefix(path, ".sloppy/artifacts/tmp/") {
		t.Fatalf("expected temp path under artifacts/tmp, got %q", path)
	}
	absPath := filepath.Join(projectDir, filepath.FromSlash(path))
	data, err := os.ReadFile(absPath)
	if err != nil {
		t.Fatalf("read temp file failed: %v", err)
	}
	if string(data) != "# temp" {
		t.Fatalf("unexpected temp file content: %q", string(data))
	}
	removed, err := s.callTool("temp_file_remove", map[string]interface{}{"path": path})
	if err != nil {
		t.Fatalf("temp_file_remove failed: %v", err)
	}
	if ok, _ := removed["removed"].(bool); !ok {
		t.Fatalf("expected removed=true, got %#v", removed["removed"])
	}
	if _, err := os.Stat(absPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected temp file removed, stat err=%v", err)
	}
}

func TestTempFileRemoveRejectsOutsideTmp(t *testing.T) {
	projectDir := t.TempDir()
	s := NewServer(projectDir)
	outside := filepath.Join(projectDir, "outside.md")
	if err := os.WriteFile(outside, []byte("x"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	_, err := s.callTool("temp_file_remove", map[string]interface{}{"path": "outside.md"})
	if err == nil {
		t.Fatal("expected temp_file_remove to reject non-temp path")
	}
}
