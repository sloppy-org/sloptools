package canvas

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"sync"
)

type SessionRecord struct {
	Opened         bool
	Mode           string
	ActiveArtifact *Event
	History        []Event
}

type Adapter struct {
	mu         sync.RWMutex
	projectDir string
	onEvent    func(Event)
	sessions   map[string]*SessionRecord
}

func newSessionRecord(opened bool) *SessionRecord {
	return &SessionRecord{
		Opened:  opened,
		Mode:    "prompt",
		History: []Event{},
	}
}

func NewAdapter(projectDir string, onEvent func(Event)) *Adapter {
	return &Adapter{
		projectDir: projectDir,
		onEvent:    onEvent,
		sessions:   map[string]*SessionRecord{},
	}
}

func (a *Adapter) ProjectDir() string {
	return a.projectDir
}

func (a *Adapter) listSessions() []string {
	ids := make([]string, 0, len(a.sessions))
	for id := range a.sessions {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *Adapter) ensureSession(sessionID string) *SessionRecord {
	r, ok := a.sessions[sessionID]
	if ok {
		return r
	}
	r = newSessionRecord(true)
	a.sessions[sessionID] = r
	return r
}

func (a *Adapter) sessionForRead(sessionID string) *SessionRecord {
	r, ok := a.sessions[sessionID]
	if ok {
		return r
	}
	return newSessionRecord(false)
}

func (a *Adapter) emit(ev Event) {
	if a.onEvent != nil {
		a.onEvent(ev)
	}
}

func (a *Adapter) CanvasSessionOpen(sessionID, modeHint string) map[string]interface{} {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)
	if modeHint != "" {
		r.Mode = modeHint
	}
	return map[string]interface{}{
		"active":               true,
		"mode":                 r.Mode,
		"mode_hint":            modeHint,
		"active_artifact_id":   activeArtifactID(r),
		"active_artifact_kind": activeArtifactKind(r),
	}
}

func activeArtifactID(r *SessionRecord) interface{} {
	if r.ActiveArtifact == nil {
		return nil
	}
	return r.ActiveArtifact.EventID
}

func activeArtifactKind(r *SessionRecord) interface{} {
	if r.ActiveArtifact == nil {
		return nil
	}
	return r.ActiveArtifact.Kind
}

func (a *Adapter) CanvasArtifactShow(sessionID, kind, title, markdownOrText, path string, page int, reason string, meta map[string]interface{}) (map[string]interface{}, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	r := a.ensureSession(sessionID)

	var ev Event
	switch kind {
	case "text":
		ev = NewEvent(EventText)
		ev.Title = title
		ev.Text = markdownOrText
	case "image":
		ev = NewEvent(EventImage)
		ev.Title = title
		ev.Path = path
	case "pdf":
		ev = NewEvent(EventPDF)
		ev.Title = title
		ev.Path = path
		ev.Page = page
	case "clear":
		ev = NewEvent(EventClear)
		ev.Reason = reason
	default:
		return nil, fmt.Errorf("unsupported kind: %s", kind)
	}
	ev.Meta = cloneMeta(meta)

	if ev.Kind == EventClear {
		r.Mode = "prompt"
		r.ActiveArtifact = nil
	} else {
		r.Mode = "review"
		r.ActiveArtifact = &ev
	}
	r.History = append(r.History, ev)
	a.emit(ev)

	return map[string]interface{}{
		"artifact_id": ev.EventID,
		"kind":        ev.Kind,
		"mode":        r.Mode,
		"artifact":    ev,
	}, nil
}

func cloneMeta(meta map[string]interface{}) map[string]interface{} {
	if len(meta) == 0 {
		return nil
	}
	cloned := make(map[string]interface{}, len(meta))
	for k, v := range meta {
		cloned[k] = v
	}
	return cloned
}

func (a *Adapter) CanvasStatus(sessionID string) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	active := map[string]interface{}(nil)
	if r.ActiveArtifact != nil {
		buf, _ := json.Marshal(r.ActiveArtifact)
		_ = json.Unmarshal(buf, &active)
	}
	return map[string]interface{}{
		"mode":                 r.Mode,
		"active":               r.Opened,
		"active_artifact_id":   activeArtifactID(r),
		"active_artifact_kind": activeArtifactKind(r),
		"active_artifact":      active,
		"history_size":         len(r.History),
	}
}

func (a *Adapter) CanvasHistory(sessionID string, limit int) map[string]interface{} {
	a.mu.RLock()
	defer a.mu.RUnlock()
	r := a.sessionForRead(sessionID)
	if limit <= 0 || limit > len(r.History) {
		limit = len(r.History)
	}
	start := len(r.History) - limit
	if start < 0 {
		start = 0
	}
	h := append([]Event(nil), r.History[start:]...)
	return map[string]interface{}{"history": h}
}

func (a *Adapter) HandleFeedback(line string) {
	line = strings.TrimSpace(line)
	if line == "" {
		return
	}
}

func (a *Adapter) ListSessions() []string {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return a.listSessions()
}
