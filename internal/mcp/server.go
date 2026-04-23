package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"

	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/canvas"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/store"
)

const (
	ServerName            = "sloppy"
	ServerVersion         = "0.2.1"
	LatestProtocolVersion = "2025-03-26"
	defaultProducerMCPURL = "http://127.0.0.1:8090/mcp"
	handoffKindFile       = "file"
	handoffKindMail       = "mail"
	tempArtifactsDirRel   = ".sloptools/artifacts/tmp"
)

var supportedProtocolVersions = map[string]struct{}{
	"2024-11-05": {},
	"2025-03-26": {},
}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	projectDir          string
	adapter             *canvas.Adapter
	handoffs            *handoffRegistry
	store               *store.Store
	groupware           *groupware.Registry
	newCalendarProvider func(ctx context.Context, account store.ExternalAccount) (tabcalendar.Provider, error)
	newEmailProvider    func(context.Context, store.ExternalAccount) (email.EmailProvider, error)
}

type handoffEnvelope struct {
	SpecVersion string                 `json:"spec_version"`
	HandoffID   string                 `json:"handoff_id"`
	Kind        string                 `json:"kind"`
	CreatedAt   string                 `json:"created_at"`
	Meta        map[string]interface{} `json:"meta"`
	Payload     map[string]interface{} `json:"payload"`
}

func NewServer(projectDir string) *Server {
	return NewServerWithStore(projectDir, nil)
}

func NewServerWithStore(projectDir string, st *store.Store) *Server {
	adapter := canvas.NewAdapter(projectDir, nil)
	srv := &Server{
		projectDir: projectDir,
		adapter:    adapter,
		handoffs:   newHandoffRegistry(),
		store:      st,
		groupware:  groupware.NewRegistry(st, ""),
	}
	srv.newCalendarProvider = func(ctx context.Context, account store.ExternalAccount) (tabcalendar.Provider, error) {
		return srv.groupware.CalendarFor(ctx, account.ID)
	}
	return srv
}

func (s *Server) ProjectDir() string {
	return s.projectDir
}

func (s *Server) SetAdapter(adapter *canvas.Adapter) {
	if adapter == nil {
		return
	}
	s.adapter = adapter
	if strings.TrimSpace(s.projectDir) == "" {
		s.projectDir = adapter.ProjectDir()
	}
}

func (s *Server) DispatchMessage(message map[string]interface{}) map[string]interface{} {
	id, hasID := message["id"]
	method, _ := message["method"].(string)
	if strings.TrimSpace(method) == "" {
		if hasID {
			return rpcErr(id, -32600, "missing method")
		}
		return nil
	}
	if !hasID {
		return nil
	}
	params, _ := message["params"].(map[string]interface{})
	if params == nil {
		params = map[string]interface{}{}
	}

	result, rerr := s.dispatch(method, params)
	if rerr != nil {
		return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": rerr}
	}
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "result": result}
}

func rpcErr(id interface{}, code int, message string) map[string]interface{} {
	return map[string]interface{}{"jsonrpc": "2.0", "id": id, "error": RPCError{Code: code, Message: message}}
}

func (s *Server) dispatch(method string, params map[string]interface{}) (map[string]interface{}, *RPCError) {
	switch method {
	case "initialize":
		requested, _ := params["protocolVersion"].(string)
		v := LatestProtocolVersion
		if _, ok := supportedProtocolVersions[requested]; ok {
			v = requested
		}
		return map[string]interface{}{
			"protocolVersion": v,
			"capabilities": map[string]interface{}{
				"tools":     map[string]interface{}{"listChanged": false},
				"resources": map[string]interface{}{"subscribe": false},
			},
			"serverInfo": map[string]interface{}{"name": ServerName, "version": ServerVersion},
		}, nil
	case "ping":
		return map[string]interface{}{}, nil
	case "tools/list":
		return map[string]interface{}{"tools": toolDefinitions()}, nil
	case "resources/list":
		return map[string]interface{}{"resources": resourcesList(s.adapter)}, nil
	case "resources/templates/list":
		return map[string]interface{}{"resourceTemplates": resourceTemplates()}, nil
	case "resources/read":
		return s.dispatchResourceRead(params)
	case "tools/call":
		return s.dispatchToolCall(params)
	default:
		return nil, &RPCError{Code: -32601, Message: "method not found: " + method}
	}
}

func (s *Server) dispatchToolCall(params map[string]interface{}) (map[string]interface{}, *RPCError) {
	name, _ := params["name"].(string)
	if strings.TrimSpace(name) == "" {
		return nil, &RPCError{Code: -32602, Message: "tools/call requires non-empty name"}
	}
	args, _ := params["arguments"].(map[string]interface{})
	if args == nil {
		args = map[string]interface{}{}
	}
	structured, err := s.callTool(name, args)
	if err != nil {
		return map[string]interface{}{
			"content": []map[string]string{{"type": "text", "text": err.Error()}},
			"isError": true,
		}, nil
	}
	b, _ := json.Marshal(structured)
	return map[string]interface{}{
		"content":           []map[string]string{{"type": "text", "text": string(b)}},
		"structuredContent": structured,
		"isError":           false,
	}, nil
}

func (s *Server) callTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	switch name {
	case "canvas_session_open", "canvas_activate":
		return s.adapter.CanvasSessionOpen(sid, strArg(args, "mode_hint")), nil
	case "canvas_artifact_show":
		text := strArg(args, "markdown_or_text")
		if text == "" {
			text = strArg(args, "text")
		}
		return s.adapter.CanvasArtifactShow(
			sid,
			strArg(args, "kind"),
			strArg(args, "title"),
			text,
			strArg(args, "path"),
			intArg(args, "page", 0),
			strArg(args, "reason"),
			nil,
		)
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
	case "handoff.create":
		return s.handoffCreate(args)
	case "handoff.peek":
		return s.handoffPeek(args)
	case "handoff.consume":
		return s.handoffConsume(args)
	case "handoff.revoke":
		return s.handoffRevoke(args)
	case "handoff.status":
		return s.handoffStatus(args)
	case "temp_file_create":
		return s.tempFileCreate(args)
	case "temp_file_remove":
		return s.tempFileRemove(args)
	case "workspace_list":
		return s.workspaceList(args)
	case "workspace_activate":
		return s.workspaceActivate(args)
	case "workspace_get":
		return s.workspaceGet(args)
	case "workspace_watch_start":
		return s.workspaceWatchStart(args)
	case "workspace_watch_stop":
		return s.workspaceWatchStop(args)
	case "workspace_watch_status":
		return s.workspaceWatchStatus(args)
	case "item_list":
		return s.itemList(args)
	case "item_get":
		return s.itemGet(args)
	case "item_create":
		return s.itemCreate(args)
	case "item_triage":
		return s.itemTriage(args)
	case "item_assign":
		return s.itemAssign(args)
	case "item_update":
		return s.itemUpdate(args)
	case "artifact_get":
		return s.artifactGet(args)
	case "artifact_list":
		return s.artifactList(args)
	case "actor_list":
		return s.actorList(args)
	case "actor_create":
		return s.actorCreate(args)
	case "calendar_list":
		return s.calendarList(args)
	case "calendar_events":
		return s.calendarEvents(args)
	case "calendar_event_create":
		return s.calendarEventCreate(args)
	case "mail_account_list":
		return s.mailAccountList(args)
	case "mail_label_list":
		return s.mailLabelList(args)
	case "mail_message_list":
		return s.mailMessageList(args)
	case "mail_message_get":
		return s.mailMessageGet(args)
	case "mail_attachment_get":
		return s.mailAttachmentGet(args)
	case "mail_action":
		return s.mailAction(args)
	case "mail_send":
		return s.mailSend(args)
	case "mail_draft_send":
		return s.mailDraftSend(args)
	case "mail_reply":
		return s.mailReply(args)
	case "mail_message_copy":
		return s.mailMessageCopy(args)
	case "mail_server_filter_list":
		return s.mailServerFilterList(args)
	case "mail_server_filter_upsert":
		return s.mailServerFilterUpsert(args)
	case "mail_server_filter_delete":
		return s.mailServerFilterDelete(args)
	default:
		return nil, errors.New("unknown tool: " + name)
	}
}

func RunStdio(projectDir string) int {
	return RunStdioWithStore(projectDir, nil)
}

func RunStdioWithStore(projectDir string, st *store.Store) int {
	s := NewServerWithStore(projectDir, st)
	reader := bufio.NewReader(os.Stdin)
	for {
		msg, framed, err := readMessage(reader)
		if err != nil {
			if errors.Is(err, io.EOF) {
				return 0
			}
			_ = writeMessage(os.Stdout, map[string]interface{}{"jsonrpc": "2.0", "id": nil, "error": RPCError{Code: -32700, Message: err.Error()}}, framed)
			continue
		}
		resp := s.DispatchMessage(msg)
		if resp == nil {
			continue
		}
		if err := writeMessage(os.Stdout, resp, framed); err != nil {
			return 1
		}
	}
}

func readMessage(r *bufio.Reader) (map[string]interface{}, bool, error) {
	line, err := r.ReadBytes('\n')
	if err != nil {
		if errors.Is(err, io.EOF) && len(line) > 0 {
			// proceed
		} else {
			return nil, true, err
		}
	}
	if len(bytes.TrimSpace(line)) == 0 {
		return nil, true, io.EOF
	}
	trimmed := bytes.TrimSpace(line)
	if len(trimmed) > 0 && trimmed[0] == '{' {
		var payload map[string]interface{}
		if err := json.Unmarshal(trimmed, &payload); err != nil {
			return nil, false, err
		}
		return payload, false, nil
	}

	headers := map[string]string{}
	for {
		t := strings.TrimSpace(string(line))
		if t == "" {
			break
		}
		parts := strings.SplitN(t, ":", 2)
		if len(parts) != 2 {
			return nil, true, fmt.Errorf("invalid header line")
		}
		headers[strings.ToLower(strings.TrimSpace(parts[0]))] = strings.TrimSpace(parts[1])
		next, err := r.ReadBytes('\n')
		if err != nil {
			return nil, true, err
		}
		line = next
	}
	lstr, ok := headers["content-length"]
	if !ok {
		return nil, true, fmt.Errorf("missing content-length header")
	}
	length, err := strconv.Atoi(lstr)
	if err != nil || length < 0 {
		return nil, true, fmt.Errorf("invalid content-length header")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(r, body); err != nil {
		return nil, true, err
	}
	var payload map[string]interface{}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, true, err
	}
	return payload, true, nil
}

func writeMessage(w io.Writer, payload map[string]interface{}, framed bool) error {
	b, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	if framed {
		if _, err := fmt.Fprintf(w, "Content-Length: %d\r\n\r\n", len(b)); err != nil {
			return err
		}
		_, err = w.Write(b)
		return err
	}
	_, err = w.Write(append(b, '\n'))
	return err
}
