package mcp

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	tabcalendar "github.com/sloppy-org/sloptools/internal/calendar"
	"github.com/sloppy-org/sloptools/internal/canvas"
	"github.com/sloppy-org/sloptools/internal/contacts"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/groupware"
	"github.com/sloppy-org/sloptools/internal/mailboxsettings"
	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/internal/tasks"
	"github.com/sloppy-org/sloptools/internal/zulip"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"
)

const compactListLimit = 10

const (
	ServerName            = "sloppy"
	ServerVersion         = "0.2.1"
	LatestProtocolVersion = "2025-03-26"
	defaultProducerMCPURL = "http://127.0.0.1:8090/mcp"
	handoffKindFile       = "file"
	handoffKindMail       = "mail"
	tempArtifactsDirRel   = ".sloptools/artifacts/tmp"
)

var supportedProtocolVersions = map[string]struct{}{"2024-11-05": {}, "2025-03-26": {}}

type RPCError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type Server struct {
	projectDir                 string
	brainConfigPath            string
	adapter                    *canvas.Adapter
	handoffs                   *handoffRegistry
	store                      *store.Store
	groupware                  *groupware.Registry
	newCalendarProvider        func(ctx context.Context, account store.ExternalAccount) (tabcalendar.Provider, error)
	newEmailProvider           func(context.Context, store.ExternalAccount) (email.EmailProvider, error)
	newMailboxSettingsProvider func(context.Context, store.ExternalAccount) (mailboxsettings.OOFProvider, error)
	newContactsProvider        func(context.Context, store.ExternalAccount) (contacts.Provider, error)
	newTasksProvider           func(context.Context, store.ExternalAccount) (tasks.Provider, error)
	newNextcloudShareClient    func(meetings.NextcloudConfig) (meetings.NextcloudShareClient, error)
	newZulipMessagesProvider   func(meetings.ZulipConfig) (zulip.MessagesProvider, error)
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
	srv := &Server{projectDir: projectDir, adapter: adapter, handoffs: newHandoffRegistry(), store: st, groupware: groupware.NewRegistry(st, "")}
	srv.newCalendarProvider = func(ctx context.Context, account store.ExternalAccount) (tabcalendar.Provider, error) {
		return srv.groupware.CalendarFor(ctx, account.ID)
	}
	srv.newTasksProvider = func(ctx context.Context, account store.ExternalAccount) (tasks.Provider, error) {
		return srv.groupware.TasksFor(ctx, account.ID)
	}
	srv.newNextcloudShareClient = meetings.NewNextcloudShareClient
	srv.newZulipMessagesProvider = defaultZulipMessagesProvider
	return srv
}

func defaultZulipMessagesProvider(cfg meetings.ZulipConfig) (zulip.MessagesProvider, error) {
	return zulip.NewClient(zulip.Config{BaseURL: cfg.BaseURL, Email: cfg.Email, APIKey: cfg.APIKey})
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
		return map[string]interface{}{"protocolVersion": v, "capabilities": map[string]interface{}{"tools": map[string]interface{}{"listChanged": false}, "resources": map[string]interface{}{"subscribe": false}}, "serverInfo": map[string]interface{}{"name": ServerName, "version": ServerVersion}}, nil
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
		return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": err.Error()}}, "isError": true}, nil
	}
	b, _ := json.Marshal(structured)
	return map[string]interface{}{"content": []map[string]string{{"type": "text", "text": string(b)}}, "structuredContent": structured, "isError": false}, nil
}

func (s *Server) callTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	sid := strArg(args, "session_id")
	for _, dispatch := range []func(string, string, map[string]interface{}) toolDispatchResult{
		s.callCanvasTool,
		s.callCoreTool,
		s.callCalendarTool,
		s.callMailTool,
		s.callContactTool,
		s.callAuxTool,
	} {
		result := dispatch(sid, name, args)
		if result.ok {
			return result.payload, result.err
		}
	}
	return nil, errors.New("unknown tool: " + name)
}

// CallTool exposes the server's tool dispatcher for in-process callers
// such as the sloptools CLI mirror commands. It returns the same
// structured payload as the MCP `tools/call` envelope's
// `structuredContent` field.
func (s *Server) CallTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	return s.callTool(name, args)
}

func RunStdio(projectDir string) int {
	return RunStdioWithStore(projectDir, nil)
}

func RunStdioWithStore(projectDir string, st *store.Store) int {
	return RunStdioWithStoreAndBrainConfig(projectDir, st, "")
}

func RunStdioWithStoreAndBrainConfig(projectDir string, st *store.Store, brainConfigPath string) int {
	s := NewServerWithStoreAndBrainConfig(projectDir, st, brainConfigPath)
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

func (s *Server) calendarList(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{"provider": store.ExternalProviderGoogleCalendar, "calendars": []map[string]interface{}{}, "count": 0}, nil
	}
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	items := make([]map[string]interface{}, 0, len(accounts))
	for _, account := range accounts {
		provider, err := s.calendarProvider(ctx, account)
		if err != nil {
			return nil, err
		}
		calendars, err := provider.ListCalendars(ctx)
		if err != nil {
			return nil, fmt.Errorf("list calendars for %q: %w", account.Label, err)
		}
		for _, cal := range calendars {
			items = append(items, map[string]interface{}{"id": cal.ID, "name": cal.Name, "description": cal.Description, "time_zone": cal.TimeZone, "primary": cal.Primary, "provider": calendarProviderName(account, provider), "account_id": account.ID, "sphere": tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, account.Sphere, allAccounts)})
		}
	}
	sort.Slice(items, func(i, j int) bool {
		return strings.ToLower(strFromAny(items[i]["name"])) < strings.ToLower(strFromAny(items[j]["name"]))
	})
	return map[string]interface{}{"provider": store.ExternalProviderGoogleCalendar, "calendars": items, "count": len(items)}, nil
}

func (s *Server) calendarEvents(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	ctx := context.Background()
	accounts, err := s.resolveCalendarAccounts(st, args)
	if err != nil {
		return nil, err
	}
	if len(accounts) == 0 {
		return map[string]interface{}{"provider": store.ExternalProviderGoogleCalendar, "events": []map[string]interface{}{}, "count": 0}, nil
	}
	allAccounts, err := tabcalendar.GoogleCalendarAccounts(st)
	if err != nil {
		return nil, err
	}
	calendarID := strings.TrimSpace(strArg(args, "calendar_id"))
	query := strings.TrimSpace(strArg(args, "query"))
	limit := intArg(args, "limit", compactListLimit)
	if limit <= 0 {
		limit = compactListLimit
	}
	rng, days, err := calendarEventsRange(args, time.Now())
	if err != nil {
		return nil, err
	}
	events := make([]map[string]interface{}, 0, limit)
	calendarNames := make(map[string]string)
	for _, account := range accounts {
		provider, err := s.calendarProvider(ctx, account)
		if err != nil {
			return nil, err
		}
		calendars, err := provider.ListCalendars(ctx)
		if err != nil {
			return nil, fmt.Errorf("list calendars for %q: %w", account.Label, err)
		}
		for _, cal := range calendars {
			if calendarID != "" && !strings.EqualFold(strings.TrimSpace(cal.ID), calendarID) {
				continue
			}
			calendarNames[cal.ID] = cal.Name
			providerSphere := tabcalendar.ResolveCalendarSphere(st, store.ExternalProviderGoogleCalendar, cal.ID, cal.Name, account.Sphere, allAccounts)
			items, err := listCalendarEvents(ctx, provider, cal.ID, rng, query, int64(limit))
			if err != nil {
				return nil, fmt.Errorf("list events for %q: %w", cal.Name, err)
			}
			for _, event := range items {
				events = append(events, eventListPayload(event, cal.Name, providerSphere, calendarProviderName(account, provider)))
			}
		}
	}
	sort.Slice(events, func(i, j int) bool {
		iStart, _ := time.Parse(time.RFC3339, strFromAny(events[i]["start"]))
		jStart, _ := time.Parse(time.RFC3339, strFromAny(events[j]["start"]))
		if iStart.Equal(jStart) {
			return strings.ToLower(strFromAny(events[i]["summary"])) < strings.ToLower(strFromAny(events[j]["summary"]))
		}
		return iStart.Before(jStart)
	})
	if len(events) > limit {
		events = events[:limit]
	}
	return map[string]interface{}{"provider": store.ExternalProviderGoogleCalendar, "calendar_id": calendarID, "calendar_name": strings.TrimSpace(calendarNames[calendarID]), "days": days, "start": rng.Start.Format(time.RFC3339), "end": rng.End.Format(time.RFC3339), "query": query, "events": events, "count": len(events)}, nil
}
