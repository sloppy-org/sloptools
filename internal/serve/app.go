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
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/sloppy-org/sloptools/internal/mcp"
	"github.com/sloppy-org/sloptools/internal/store"
)

const (
	DefaultHost = "127.0.0.1"
	DefaultPort = 9420
)

type App struct {
	ProjectDir string
	Server     *mcp.Server
	Store      *store.Store

	httpServer   *http.Server
	shutdownDone chan struct{}
}

func NewApp(projectDir, dataDir string) *App {
	a := &App{
		ProjectDir:   projectDir,
		shutdownDone: make(chan struct{}),
	}
	if strings.TrimSpace(dataDir) != "" {
		dbPath := filepath.Join(dataDir, "sloptools.db")
		if st, err := store.New(dbPath); err == nil {
			a.Store = st
		} else {
			fmt.Printf("domain MCP tools disabled: store unavailable (%v)\n", err)
		}
	}
	a.Server = mcp.NewServerWithStore(projectDir, a.Store)
	return a
}

func (a *App) Router() http.Handler {
	r := chi.NewRouter()
	r.Post("/mcp", a.handleMCPPost)
	r.Get("/mcp", a.handleMCPGet)
	r.Delete("/mcp", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })
	r.Get("/files/*", a.handleFiles)
	r.Get("/health", a.handleHealth)
	return r
}

func (a *App) handleMCPPost(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	var req map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, map[string]interface{}{"jsonrpc": "2.0", "id": nil, "error": map[string]interface{}{"code": -32700, "message": "parse error"}})
		return
	}
	resp := a.Server.DispatchMessage(req)
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
	writeJSON(w, map[string]interface{}{
		"status":      "ok",
		"project_dir": a.ProjectDir,
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

func (a *App) Stop(ctx context.Context) error {
	var shutdownErr error
	if a.httpServer != nil {
		shutdownErr = a.httpServer.Shutdown(ctx)
	}
	if a.Store != nil {
		shutdownErr = errors.Join(shutdownErr, a.Store.Close())
		a.Store = nil
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
