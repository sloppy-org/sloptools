package mcp

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/sloppy-org/sloptools/internal/canvas"
	"github.com/sloppy-org/sloptools/internal/surface"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"
)

func isPathWithinDir(path, dir string) bool {
	rel, err := filepath.Rel(dir, path)
	if err != nil {
		return false
	}
	rel = filepath.Clean(rel)
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func (s *Server) dispatchCanvas(sid string, name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "canvas_session_open", "canvas_activate":
		return s.adapter.CanvasSessionOpen(sid, strArg(args, "mode_hint")), nil
	case "canvas_artifact_show":
		text := strArg(args, "markdown_or_text")
		if text == "" {
			text = strArg(args, "text")
		}
		return s.adapter.CanvasArtifactShow(sid, strArg(args, "kind"), strArg(args, "title"), text, strArg(args, "path"), intArg(args, "page", 0), strArg(args, "reason"), nil)
	case "canvas_render_text":
		text := strArg(args, "markdown_or_text")
		if text == "" {
			text = strArg(args, "text")
		}
		return s.adapter.CanvasArtifactShow(sid, "text", strArg(args, "title"), text, "", 0, "", nil)
	case "canvas_render_image":
		return s.adapter.CanvasArtifactShow(sid, "image", strArg(args, "title"), "", strArg(args, "path"), 0, "", nil)
	case "canvas_render_pdf":
		return s.adapter.CanvasArtifactShow(sid, "pdf", strArg(args, "title"), "", strArg(args, "path"), intArg(args, "page", 0), "", nil)
	case "canvas_clear":
		return s.adapter.CanvasArtifactShow(sid, "clear", "", "", "", 0, strArg(args, "reason"), nil)
	case "canvas_status":
		return s.adapter.CanvasStatus(sid), nil
	case "canvas_history":
		return s.adapter.CanvasHistory(sid, intArg(args, "limit", 20)), nil
	case "canvas_import_handoff":
		return s.canvasImportHandoff(sid, args)
	}
	return nil, fmt.Errorf("unknown canvas tool: %s", name)
}

func (s *Server) resolveTempArtifactsDir(cwdArg string) (string, string, error) {
	cwd := strings.TrimSpace(cwdArg)
	if cwd == "" {
		cwd = s.adapter.ProjectDir()
	}
	if strings.TrimSpace(cwd) == "" {
		cwd = "."
	}
	rootAbs, err := filepath.Abs(cwd)
	if err != nil {
		return "", "", err
	}
	tmpAbs := filepath.Clean(filepath.Join(rootAbs, tempArtifactsDirRel))
	if !isPathWithinDir(tmpAbs, rootAbs) {
		return "", "", errors.New("temp artifacts directory escapes project root")
	}
	return rootAbs, tmpAbs, nil
}

func (s *Server) tempFileCreate(args map[string]interface{}) (map[string]interface{}, error) {
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(tmpAbs, 0755); err != nil {
		return nil, err
	}
	prefix := strings.TrimSpace(strArg(args, "prefix"))
	if prefix == "" {
		prefix = "tmp"
	}
	prefix = strings.ReplaceAll(prefix, string(os.PathSeparator), "-")
	prefix = strings.ReplaceAll(prefix, "/", "-")
	suffix := strings.TrimSpace(strArg(args, "suffix"))
	if suffix == "" {
		suffix = ".md"
	}
	suffix = strings.ReplaceAll(suffix, string(os.PathSeparator), "")
	suffix = strings.ReplaceAll(suffix, "/", "")
	pattern := prefix + "-*" + suffix
	f, err := os.CreateTemp(tmpAbs, pattern)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	content := strArg(args, "content")
	if content != "" {
		if _, err := f.WriteString(content); err != nil {
			return nil, err
		}
	}
	absPath := filepath.Clean(f.Name())
	relPath, err := filepath.Rel(rootAbs, absPath)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"ok": true, "path": filepath.ToSlash(relPath), "abs_path": absPath}, nil
}

func (s *Server) tempFileRemove(args map[string]interface{}) (map[string]interface{}, error) {
	target := strings.TrimSpace(strArg(args, "path"))
	if target == "" {
		return nil, errors.New("path is required")
	}
	rootAbs, tmpAbs, err := s.resolveTempArtifactsDir(strArg(args, "cwd"))
	if err != nil {
		return nil, err
	}
	var absPath string
	if filepath.IsAbs(target) {
		absPath = filepath.Clean(target)
	} else {
		absPath = filepath.Clean(filepath.Join(rootAbs, target))
	}
	if !isPathWithinDir(absPath, tmpAbs) {
		return nil, errors.New("path must be under .sloptools/artifacts/tmp")
	}
	err = os.Remove(absPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	removed := err == nil
	relPath, relErr := filepath.Rel(rootAbs, absPath)
	if relErr != nil {
		relPath = absPath
	}
	return map[string]interface{}{"ok": true, "path": filepath.ToSlash(relPath), "removed": removed}, nil
}

func (s *Server) canvasImportHandoff(sessionID string, args map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(sessionID) == "" {
		return nil, errors.New("session_id is required")
	}
	handoffID := strArg(args, "handoff_id")
	if strings.TrimSpace(handoffID) == "" {
		return nil, errors.New("handoff_id is required")
	}
	producerMCPURL := strArg(args, "producer_mcp_url")
	if strings.TrimSpace(producerMCPURL) == "" {
		producerMCPURL = defaultProducerMCPURL
	}
	peek, err := mcpToolCall(producerMCPURL, "handoff.peek", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		return nil, fmt.Errorf("handoff.peek failed: %w", err)
	}
	consume, err := mcpToolCall(producerMCPURL, "handoff.consume", map[string]interface{}{"handoff_id": handoffID})
	if err != nil {
		return nil, fmt.Errorf("handoff.consume failed: %w", err)
	}
	env, err := decodeEnvelope(consume)
	if err != nil {
		return nil, err
	}
	peekKind := strings.TrimSpace(fmt.Sprint(peek["kind"]))
	if peekKind != "" && peekKind != env.Kind {
		return nil, fmt.Errorf("handoff kind changed between peek and consume: %s != %s", peekKind, env.Kind)
	}
	title := strings.TrimSpace(strArg(args, "title"))
	switch env.Kind {
	case handoffKindFile:
		return s.importFile(sessionID, handoffID, title, env)
	default:
		return nil, fmt.Errorf("unsupported handoff kind: %s", env.Kind)
	}
}

func mcpToolCall(mcpURL, name string, arguments map[string]interface{}) (map[string]interface{}, error) {
	request := map[string]interface{}{"jsonrpc": "2.0", "id": 1, "method": "tools/call", "params": map[string]interface{}{"name": name, "arguments": arguments}}
	body, _ := json.Marshal(request)
	client := &http.Client{Timeout: 20 * time.Second}
	resp, err := client.Post(mcpURL, "application/json", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var rpcResp map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&rpcResp); err != nil {
		return nil, err
	}
	if rpcErr, ok := rpcResp["error"].(map[string]interface{}); ok {
		return nil, fmt.Errorf("%v", rpcErr["message"])
	}
	result, _ := rpcResp["result"].(map[string]interface{})
	if result == nil {
		return nil, errors.New("missing result")
	}
	if isErr, _ := result["isError"].(bool); isErr {
		if sc, ok := result["structuredContent"].(map[string]interface{}); ok {
			if msg, ok := sc["error"].(string); ok && strings.TrimSpace(msg) != "" {
				return nil, errors.New(msg)
			}
		}
		return nil, errors.New("remote tool returned error")
	}
	structured, _ := result["structuredContent"].(map[string]interface{})
	if structured == nil {
		return nil, errors.New("missing structuredContent")
	}
	return structured, nil
}

func decodeEnvelope(payload map[string]interface{}) (handoffEnvelope, error) {
	raw, _ := json.Marshal(payload)
	var env handoffEnvelope
	if err := json.Unmarshal(raw, &env); err != nil {
		return handoffEnvelope{}, fmt.Errorf("invalid handoff envelope: %w", err)
	}
	if strings.TrimSpace(env.Kind) == "" {
		return handoffEnvelope{}, errors.New("handoff envelope missing kind")
	}
	if env.Meta == nil {
		env.Meta = map[string]interface{}{}
	}
	if env.Payload == nil {
		env.Payload = map[string]interface{}{}
	}
	return env, nil
}

func (s *Server) importFile(sessionID, handoffID, title string, env handoffEnvelope) (map[string]interface{}, error) {
	contentB64 := strings.TrimSpace(fmt.Sprint(env.Payload["content_base64"]))
	if contentB64 == "" || contentB64 == "<nil>" {
		return nil, errors.New("file payload missing content_base64")
	}
	content, err := base64.StdEncoding.DecodeString(contentB64)
	if err != nil {
		return nil, fmt.Errorf("invalid file payload base64: %w", err)
	}
	if err := verifyFileIntegrity(env.Meta, content); err != nil {
		return nil, err
	}
	filename := sanitizeFilename(strings.TrimSpace(fmt.Sprint(env.Meta["filename"])))
	if filename == "" || filename == "<nil>" {
		filename = "handoff-file"
	}
	mimeType := strings.TrimSpace(fmt.Sprint(env.Meta["mime_type"]))
	if mimeType == "" || mimeType == "<nil>" {
		mimeType = mime.TypeByExtension(filepath.Ext(filename))
	}
	if strings.TrimSpace(mimeType) == "" {
		mimeType = "application/octet-stream"
	}
	if strings.TrimSpace(title) == "" {
		title = filename
	}
	relativePath, err := s.writeImportedFile(handoffID, filename, content)
	if err != nil {
		return nil, err
	}
	var shown map[string]interface{}
	switch {
	case mimeType == "application/pdf":
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "pdf", title, "", relativePath, 0, "", nil)
	case strings.HasPrefix(mimeType, "image/"):
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "image", title, "", relativePath, 0, "", nil)
	case strings.HasPrefix(mimeType, "text/") && utf8.Valid(content):
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "text", title, string(content), "", 0, "", nil)
	default:
		summary := fmt.Sprintf("# Imported File\n\n- Filename: `%s`\n- MIME: `%s`\n- Size: `%d` bytes\n- Stored at: `%s`\n\nPreview not available for this file type.", filename, mimeType, len(content), relativePath)
		shown, err = s.adapter.CanvasArtifactShow(sessionID, "text", title, summary, "", 0, "", nil)
	}
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"artifact_id": shown["artifact_id"], "title": title, "handoff_id": handoffID, "kind": env.Kind, "mime_type": mimeType, "path": relativePath, "size_bytes": len(content)}, nil
}

func verifyFileIntegrity(meta map[string]interface{}, content []byte) error {
	if meta == nil {
		return nil
	}
	if raw, ok := meta["size_bytes"]; ok {
		want, has := asInt(raw)
		if has && want >= 0 && len(content) != want {
			return fmt.Errorf("file size mismatch: expected %d, got %d", want, len(content))
		}
	}
	hash := strings.ToLower(strings.TrimSpace(fmt.Sprint(meta["sha256"])))
	if hash != "" && hash != "<nil>" {
		sum := sha256.Sum256(content)
		if fmt.Sprintf("%x", sum) != hash {
			return errors.New("file sha256 mismatch")
		}
	}
	return nil
}

func asInt(raw interface{}) (int, bool) {
	switch v := raw.(type) {
	case int:
		return v, true
	case int32:
		return int(v), true
	case int64:
		return int(v), true
	case float64:
		return int(v), true
	default:
		return 0, false
	}
}

func sanitizeFilename(name string) string {
	if name == "" {
		return ""
	}
	base := filepath.Base(name)
	var b strings.Builder
	for _, r := range base {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteRune('_')
		}
	}
	return strings.TrimSpace(b.String())
}

func (s *Server) writeImportedFile(handoffID, filename string, content []byte) (string, error) {
	projectDir := s.adapter.ProjectDir()
	if strings.TrimSpace(projectDir) == "" {
		return "", errors.New("project directory not configured")
	}
	importDir := filepath.Join(projectDir, ".sloptools", "artifacts", "imports")
	if err := os.MkdirAll(importDir, 0o755); err != nil {
		return "", err
	}
	prefix := handoffID
	if len(prefix) > 12 {
		prefix = prefix[:12]
	}
	safeName := sanitizeFilename(filename)
	if safeName == "" {
		safeName = "artifact.bin"
	}
	fullPath := filepath.Join(importDir, prefix+"-"+safeName)
	if err := os.WriteFile(fullPath, content, 0o644); err != nil {
		return "", err
	}
	rel, err := filepath.Rel(projectDir, fullPath)
	if err != nil {
		return "", err
	}
	return filepath.ToSlash(rel), nil
}

func (s *Server) dispatchResourceRead(params map[string]interface{}) (map[string]interface{}, *RPCError) {
	uri, _ := params["uri"].(string)
	if strings.TrimSpace(uri) == "" {
		return nil, &RPCError{Code: -32602, Message: "resources/read requires uri"}
	}
	content, err := readResource(s.adapter, uri)
	if err != nil {
		return nil, &RPCError{Code: -32002, Message: err.Error()}
	}
	return map[string]interface{}{"contents": []map[string]interface{}{content}}, nil
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return v
}

func intArg(args map[string]interface{}, key string, def int) int {
	v, ok := args[key]
	if !ok {
		return def
	}
	switch t := v.(type) {
	case float64:
		return int(t)
	case int:
		return t
	case int64:
		return int(t)
	default:
		return def
	}
}

func toolDefinitions() []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(surface.MCPTools))
	for _, tool := range surface.MCPTools {
		schema := map[string]interface{}{"type": "object"}
		if len(tool.Required) > 0 {
			schema["required"] = append([]string(nil), tool.Required...)
		}
		if len(tool.Properties) > 0 {
			props := make(map[string]interface{}, len(tool.Properties))
			for k, v := range tool.Properties {
				prop := map[string]interface{}{"type": v.Type, "description": v.Description}
				if len(v.Enum) > 0 {
					prop["enum"] = v.Enum
				}
				props[k] = prop
			}
			schema["properties"] = props
		}
		applyToolSchemaDefaults(tool.Name, schema)
		out = append(out, map[string]interface{}{"name": tool.Name, "description": tool.Description, "inputSchema": schema})
	}
	return out
}

func resourceTemplates() []map[string]interface{} {
	return []map[string]interface{}{{"uriTemplate": "sloptools://session/{session_id}", "name": "Canvas Session Status", "mimeType": "application/json", "description": "Current status for a canvas session."}, {"uriTemplate": "sloptools://session/{session_id}/history", "name": "Canvas Session History", "mimeType": "application/json", "description": "Recent event history for a canvas session."}}
}

func resourcesList(adapter *canvas.Adapter) []map[string]interface{} {
	out := []map[string]interface{}{}
	for _, sid := range adapter.ListSessions() {
		for _, uri := range []string{"sloptools://session/" + sid, "sloptools://session/" + sid + "/history"} {
			out = append(out, map[string]interface{}{"uri": uri, "name": uri, "mimeType": "application/json"})
		}
	}
	return out
}

func readResource(adapter *canvas.Adapter, uri string) (map[string]interface{}, error) {
	if !strings.HasPrefix(uri, "sloptools://session/") {
		return nil, fmt.Errorf("unsupported uri: %s", uri)
	}
	path := strings.TrimPrefix(uri, "sloptools://session/")
	if path == "" {
		return nil, fmt.Errorf("missing session id")
	}
	parts := strings.Split(path, "/")
	sid := parts[0]
	var payload map[string]interface{}
	if len(parts) == 1 {
		payload = adapter.CanvasStatus(sid)
	} else {
		switch parts[1] {
		case "history":
			payload = adapter.CanvasHistory(sid, 100)
		default:
			return nil, fmt.Errorf("unsupported session resource: %s", uri)
		}
	}
	b, _ := json.Marshal(payload)
	return map[string]interface{}{"uri": uri, "mimeType": "application/json", "text": string(b)}, nil
}
