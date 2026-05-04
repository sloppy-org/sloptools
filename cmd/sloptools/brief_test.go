package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/calendarbrief"
)

func TestJSONLineEmitterWritesOnePayloadPerLine(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "channel.jsonl")
	sink, err := os.Create(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	t.Cleanup(func() { sink.Close() })
	emit := jsonLineEmitter(sink)
	notification := calendarbrief.Notification{
		EventID:    "evt-1",
		EventTitle: "Plasma Orga sync",
		EventStart: time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC),
		Briefs:     []map[string]interface{}{{"person": "Ada"}},
	}
	if err := emit(context.Background(), notification); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := sink.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read sink: %v", err)
	}
	lines := strings.Split(strings.TrimRight(string(body), "\n"), "\n")
	if len(lines) != 1 {
		t.Fatalf("lines = %d, want 1", len(lines))
	}
	var got calendarbrief.Notification
	if err := json.Unmarshal([]byte(lines[0]), &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.EventID != "evt-1" || len(got.Briefs) != 1 {
		t.Fatalf("emitted payload = %#v", got)
	}
}

func TestHTTPPostEmitterPostsJSONBody(t *testing.T) {
	var captured []byte
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s, want POST", r.Method)
		}
		if got := r.Header.Get("Content-Type"); got != "application/json" {
			t.Errorf("content-type = %q, want application/json", got)
		}
		buf := make([]byte, 1024)
		n, _ := r.Body.Read(buf)
		captured = append(captured, buf[:n]...)
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	emit := httpPostEmitter(server.Client(), server.URL)
	notification := calendarbrief.Notification{
		EventID:    "evt-http",
		EventTitle: "Sync",
		EventStart: time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC),
	}
	if err := emit(context.Background(), notification); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if !strings.Contains(string(captured), "evt-http") {
		t.Fatalf("server body = %q, want event id", captured)
	}
}

func TestHTTPPostEmitterReportsErrorStatus(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	t.Cleanup(server.Close)
	emit := httpPostEmitter(server.Client(), server.URL)
	err := emit(context.Background(), calendarbrief.Notification{EventID: "evt-fail"})
	if err == nil {
		t.Fatal("expected error from 500 response")
	}
	if !strings.Contains(err.Error(), "500") {
		t.Fatalf("err = %v, want status code in message", err)
	}
}

func TestNewBriefEmitterPicksHTTPWhenNotifyURLPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(server.Close)
	emit, err := newBriefEmitter(server.URL, os.Stdout)
	if err != nil {
		t.Fatalf("newBriefEmitter: %v", err)
	}
	if err := emit(context.Background(), calendarbrief.Notification{EventID: "evt-x"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
}

func TestNewBriefEmitterFallsBackToStdoutWhenNotifyURLEmpty(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "stdout.jsonl")
	sink, err := os.Create(path)
	if err != nil {
		t.Fatalf("create sink: %v", err)
	}
	t.Cleanup(func() { sink.Close() })
	emit, err := newBriefEmitter("   ", sink)
	if err != nil {
		t.Fatalf("newBriefEmitter: %v", err)
	}
	if err := emit(context.Background(), calendarbrief.Notification{EventID: "evt-stdout"}); err != nil {
		t.Fatalf("emit: %v", err)
	}
	if err := sink.Sync(); err != nil {
		t.Fatalf("sync: %v", err)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !strings.Contains(string(body), "evt-stdout") {
		t.Fatalf("stdout sink = %q, want event id", body)
	}
}
