package serve

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/gorilla/websocket"
	"github.com/sloppy-org/sloptools/internal/canvas"
	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/store"
)

// syscallUmask wraps syscall.Umask so the StartUnix path can be unit-tested
// without dragging in OS-specific build tags. syscall.Umask is identical on
// Linux and Darwin (the only platforms sloptools ships to).
func syscallUmask(mask int) int { return syscall.Umask(mask) }

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 9420
)

type App struct {
	ProjectDir string
	Adapter    *canvas.Adapter
	Server     *mcp.Server
	Store      *store.Store

	mu             sync.Mutex
	pending        []canvas.Event
	wsClients      map[*websocket.Conn]struct{}
	wsUpgrader     websocket.Upgrader
	httpServer     *http.Server
	unixSocketPath string
	shutdownDone   chan struct{}
}

func NewApp(projectDir, dataDir string) *App {
	a := &App{
		ProjectDir:   projectDir,
		wsClients:    map[*websocket.Conn]struct{}{},
		wsUpgrader:   websocket.Upgrader{CheckOrigin: func(r *http.Request) bool { return true }},
		shutdownDone: make(chan struct{}),
	}
	a.Adapter = canvas.NewAdapter(projectDir, a.queueEvent)
	if strings.TrimSpace(dataDir) != "" {
		dbPath := filepath.Join(dataDir, "sloppy.db")
		if st, err := store.New(dbPath); err == nil {
			a.Store = st
		} else {
			fmt.Printf("domain MCP tools disabled: store unavailable (%v)\n", err)
		}
	}
	a.Server = mcp.NewServerWithStore(projectDir, a.Store)
	a.Server.SetAdapter(a.Adapter)
	return a
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/mcp", a.handleMCPPost)
	r.Get("/mcp", a.handleMCPGet)
	r.Delete("/mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Get("/ws/canvas", a.handleCanvasWS)
	r.Get("/files/*", a.handleFiles)
	r.Get("/health", a.handleHealth)
	return r
}

func (a *App) queueEvent(ev canvas.Event) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pending = append(a.pending, ev)
}

func (a *App) flushEvents() []canvas.Event {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := append([]canvas.Event(nil), a.pending...)
	a.pending = a.pending[:0]
	return out
}

func (a *App) broadcastPending() {
	events := a.flushEvents()
	if len(events) == 0 {
		return
	}
	a.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(a.wsClients))
	for ws := range a.wsClients {
		clients = append(clients, ws)
	}
	a.mu.Unlock()
	for _, ev := range events {
		b, _ := json.Marshal(ev)
		for _, ws := range clients {
			_ = ws.WriteMessage(websocket.TextMessage, b)
		}
	}
}

func (a *App) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"jsonrpc": "2.0", "id": nil, "error": map[string]interface{}{"code": -32700, "message": "parse error"}})
		return
	}
	resp := a.Server.DispatchMessage(req)
	a.broadcastPending()
	if resp == nil {
		w.WriteHeader(http.StatusAccepted)
		return
	}
	if method, _ := req["method"].(string); method == "initialize" {
		w.Header().Set("Mcp-Session-Id", randomSessionID())
	}
	writeJSON(w, resp)
}

func (a *App) handleMCPGet(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, ok := w.(http.Flusher)
	if !ok {
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	t := time.NewTicker(30 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-t.C:
			_, _ = w.Write([]byte(": keepalive\n\n"))
			f.Flush()
		}
	}
}

func (a *App) handleCanvasWS(w http.ResponseWriter, r *http.Request) {
	ws, err := a.wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	a.mu.Lock()
	a.wsClients[ws] = struct{}{}
	a.mu.Unlock()
	defer func() {
		a.mu.Lock()
		delete(a.wsClients, ws)
		a.mu.Unlock()
		_ = ws.Close()
	}()
	for {
		_, msg, err := ws.ReadMessage()
		if err != nil {
			return
		}
		a.Adapter.HandleFeedback(string(msg))
	}
}

func (a *App) handleFiles(w http.ResponseWriter, r *http.Request) {
	rawPath := strings.TrimPrefix(chi.URLParam(r, "*"), "/")
	raw, err := url.PathUnescape(rawPath)
	if err != nil {
		http.Error(w, "invalid path encoding", http.StatusBadRequest)
		return
	}
	raw = filepath.ToSlash(strings.TrimPrefix(raw, "/"))
	cleanProjectDir := filepath.Clean(a.ProjectDir)
	projectPrefix := strings.TrimPrefix(filepath.ToSlash(cleanProjectDir), "/")
	if strings.HasPrefix(raw, projectPrefix+"/") {
		raw = strings.TrimPrefix(raw, projectPrefix+"/")
	}
	if raw == "" {
		http.Error(w, "missing path", http.StatusBadRequest)
		return
	}
	full := filepath.Clean(filepath.Join(cleanProjectDir, filepath.FromSlash(raw)))
	if !strings.HasPrefix(full, cleanProjectDir+string(os.PathSeparator)) {
		http.Error(w, "access denied", http.StatusForbidden)
		return
	}
	st, err := os.Stat(full)
	if err != nil || st.IsDir() {
		http.Error(w, "not found", http.StatusNotFound)
		return
	}
	http.ServeFile(w, r, full)
}

func (a *App) handleHealth(w http.ResponseWriter, _ *http.Request) {
	a.mu.Lock()
	wsCount := len(a.wsClients)
	a.mu.Unlock()
	writeJSON(w, map[string]interface{}{
		"status":      "ok",
		"project_dir": a.ProjectDir,
		"sessions":    a.Adapter.ListSessions(),
		"ws_clients":  wsCount,
	})
}

func (a *App) Start(host string, port int) error {
	addr := fmt.Sprintf("%s:%d", host, port)
	a.httpServer = &http.Server{
		Addr:              addr,
		Handler:           a.Router(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Println("sloptools mcp listener listening on:")
	for _, u := range ListenURLs(host, port) {
		fmt.Printf("  %s\n", u)
	}
	fmt.Printf("  MCP endpoint: http://%s/mcp\n", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	fmt.Printf("  project dir:  %s\n", a.ProjectDir)
	err := a.httpServer.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

// StartUnix listens on a Unix domain socket at `socketPath`, with 0600
// permissions enforced before the listener accepts connections. This is the
// preferred transport for personal MCP tools (mail, etc.) on shared boxes —
// only the file's owning user (and root) can `connect()`. There is no
// network listener at all.
func (a *App) StartUnix(socketPath string) error {
	if socketPath == "" {
		return errors.New("StartUnix requires a non-empty socket path")
	}
	cleaned := filepath.Clean(socketPath)
	a.unixSocketPath = cleaned
	socketDir := filepath.Dir(cleaned)
	if err := os.MkdirAll(socketDir, 0700); err != nil {
		return fmt.Errorf("create socket parent dir: %w", err)
	}
	if err := os.Chmod(socketDir, 0700); err != nil {
		return fmt.Errorf("chmod socket parent dir: %w", err)
	}
	// Remove any stale socket from a previous crash. Don't touch non-socket
	// files at the same path — that's almost certainly a misconfiguration.
	if st, err := os.Lstat(cleaned); err == nil {
		if st.Mode()&os.ModeSocket == 0 {
			return fmt.Errorf("refusing to remove non-socket file at %s", cleaned)
		}
		if err := os.Remove(cleaned); err != nil {
			return fmt.Errorf("remove stale socket: %w", err)
		}
	}
	// Tighten umask so the listener's file lands at 0600 even if the kernel
	// applies a default mask. Belt-and-suspenders with the explicit Chmod
	// below.
	oldMask := syscallUmask(0177)
	listener, err := net.Listen("unix", cleaned)
	syscallUmask(oldMask)
	if err != nil {
		return fmt.Errorf("listen on unix socket: %w", err)
	}
	if err := os.Chmod(cleaned, 0600); err != nil {
		_ = listener.Close()
		return fmt.Errorf("chmod socket: %w", err)
	}
	a.httpServer = &http.Server{
		Handler:           a.Router(),
		ReadHeaderTimeout: 15 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      60 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Println("sloptools mcp listener listening on:")
	fmt.Printf("  http+unix://%s\n", cleaned)
	fmt.Printf("  MCP endpoint: http+unix://%s/mcp (mode 0600)\n", cleaned)
	fmt.Printf("  project dir:  %s\n", a.ProjectDir)
	err = a.httpServer.Serve(listener)
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}

func (a *App) Stop(ctx context.Context) error {
	var shutdownErr error
	if a.httpServer != nil {
		shutdownErr = a.httpServer.Shutdown(ctx)
	}
	if a.Store != nil {
		shutdownErr = errors.Join(shutdownErr, a.Store.Close())
		a.Store = nil
	}
	if a.unixSocketPath != "" {
		_ = os.Remove(a.unixSocketPath)
	}
	return shutdownErr
}

func writeJSON(w http.ResponseWriter, payload interface{}) {
	w.Header().Set("Content-Type", "application/json")
	enc := json.NewEncoder(w)
	_ = enc.Encode(payload)
}

func randomSessionID() string {
	return fmt.Sprintf("%d", time.Now().UnixNano())
}

func ListenURLs(host string, port int) []string {
	return ListenURLsWithScheme(host, port, "http")
}

func ListenURLsWithScheme(host string, port int, scheme string) []string {
	cleanScheme := strings.TrimSpace(strings.ToLower(scheme))
	if cleanScheme == "" {
		cleanScheme = "http"
	}
	if host != "0.0.0.0" && host != "::" {
		return []string{fmt.Sprintf("%s://%s:%d", cleanScheme, host, port)}
	}
	urls := []string{fmt.Sprintf("%s://localhost:%d", cleanScheme, port)}
	ifaces, _ := net.Interfaces()
	for _, iface := range ifaces {
		if iface.Flags&net.FlagUp == 0 || iface.Flags&net.FlagLoopback != 0 {
			continue
		}
		addrs, _ := iface.Addrs()
		for _, addr := range addrs {
			ipnet, ok := addr.(*net.IPNet)
			if !ok || ipnet.IP == nil || ipnet.IP.To4() == nil {
				continue
			}
			urls = append(urls, fmt.Sprintf("%s://%s:%d", cleanScheme, ipnet.IP.String(), port))
		}
	}
	return urls
}
