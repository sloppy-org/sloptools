package meetings

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNewWatcherRefusesNonCanonicalHost(t *testing.T) {
	cfg := SphereConfig{Inbox: t.TempDir(), CanonicalHost: "mailuefterl"}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	if _, err := NewWatcher(cfg, "laptop-2", pipeline, 0); err == nil {
		t.Fatal("expected canonical-host refusal")
	} else if hostErr := new(CanonicalHostError); !errors.As(err, &hostErr) {
		t.Fatalf("expected CanonicalHostError, got %T: %v", err, err)
	}
}

func TestNewWatcherAcceptsMatchingHostCaseInsensitive(t *testing.T) {
	cfg := SphereConfig{Inbox: t.TempDir(), CanonicalHost: "Mailuefterl"}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	if _, err := NewWatcher(cfg, "MAILUEFTERL", pipeline, 0); err != nil {
		t.Fatalf("matching host must accept, got %v", err)
	}
}

func TestNewWatcherWithoutCanonicalHostSkipsCheck(t *testing.T) {
	cfg := SphereConfig{Inbox: t.TempDir()}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	if _, err := NewWatcher(cfg, "any-host", pipeline, 0); err != nil {
		t.Fatalf("no canonical_host should not enforce, got %v", err)
	}
}

func TestRunOnceProcessesAndDeletesAudioOnSuccess(t *testing.T) {
	inbox := t.TempDir()
	audio := filepath.Join(inbox, "memo-001.m4a")
	mustWriteBytes(t, audio, []byte("audio"))
	mustWriteBytes(t, filepath.Join(inbox, "ignore.txt"), []byte("not audio"))

	var seen []string
	pipeline := AudioPipelineFunc(func(_ context.Context, path string) error {
		seen = append(seen, path)
		return nil
	})
	w := mustWatcher(t, SphereConfig{Inbox: inbox}, "host", pipeline)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if len(seen) != 1 || seen[0] != audio {
		t.Fatalf("processed = %v", seen)
	}
	if _, err := os.Stat(audio); !os.IsNotExist(err) {
		t.Fatalf("audio must be deleted after success, stat err = %v", err)
	}
	if _, err := os.Stat(audio + FailedSidecarSuffix); !os.IsNotExist(err) {
		t.Fatalf("no sidecar expected on success, stat err = %v", err)
	}
}

func TestRunOnceLeavesAudioAndWritesSidecarOnFailure(t *testing.T) {
	inbox := t.TempDir()
	audio := filepath.Join(inbox, "memo-fail.m4a")
	mustWriteBytes(t, audio, []byte("audio"))
	pipeline := AudioPipelineFunc(func(context.Context, string) error {
		return errors.New("transcribe failed: model missing")
	})
	w := mustWatcher(t, SphereConfig{Inbox: inbox}, "host", pipeline)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if _, err := os.Stat(audio); err != nil {
		t.Fatalf("audio must remain after failure: %v", err)
	}
	sidecar, err := os.ReadFile(audio + FailedSidecarSuffix)
	if err != nil {
		t.Fatalf("read sidecar: %v", err)
	}
	if !strings.Contains(string(sidecar), "transcribe failed: model missing") {
		t.Fatalf("sidecar missing root cause: %q", sidecar)
	}
	if !strings.Contains(string(sidecar), filepath.Base(audio)) {
		t.Fatalf("sidecar must reference filename: %q", sidecar)
	}
}

func TestRunOnceSkipsFilesAlreadyMarkedFailed(t *testing.T) {
	inbox := t.TempDir()
	audio := filepath.Join(inbox, "memo-old.m4a")
	mustWriteBytes(t, audio, []byte("audio"))
	mustWriteBytes(t, audio+FailedSidecarSuffix, []byte("prior failure"))

	called := 0
	pipeline := AudioPipelineFunc(func(context.Context, string) error {
		called++
		return nil
	})
	w := mustWatcher(t, SphereConfig{Inbox: inbox}, "host", pipeline)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if called != 0 {
		t.Fatalf("expected pipeline to be skipped for previously-failed audio, got %d invocations", called)
	}
}

func TestRunOnceMissingInboxIsNotAnError(t *testing.T) {
	cfg := SphereConfig{Inbox: filepath.Join(t.TempDir(), "absent")}
	called := 0
	pipeline := AudioPipelineFunc(func(context.Context, string) error {
		called++
		return nil
	})
	w := mustWatcher(t, cfg, "host", pipeline)
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce: %v", err)
	}
	if called != 0 {
		t.Fatalf("missing inbox should not invoke pipeline, got %d", called)
	}
}

func TestRunOnceSeedsMeetingNotesWithoutFiringIngester(t *testing.T) {
	inbox := t.TempDir()
	root := t.TempDir()
	pre := filepath.Join(root, "2026-04-29-standup", "MEETING_NOTES.md")
	mustWriteBytes(t, pre, []byte("# Standup\n"))
	cfg := SphereConfig{Inbox: inbox, MeetingsRoot: root}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	w := mustWatcher(t, cfg, "host", pipeline)
	called := 0
	w.SetNotesIngester(func(_ context.Context, _ string) error {
		called++
		return nil
	})
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("RunOnce seed: %v", err)
	}
	if called != 0 {
		t.Fatalf("seed scan must not call ingester, got %d", called)
	}
}

func TestRunOnceCallsNotesIngesterOnMtimeAdvance(t *testing.T) {
	inbox := t.TempDir()
	root := t.TempDir()
	notePath := filepath.Join(root, "2026-04-29-standup", "MEETING_NOTES.md")
	mustWriteBytes(t, notePath, []byte("# Standup\n"))
	cfg := SphereConfig{Inbox: inbox, MeetingsRoot: root}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	w := mustWatcher(t, cfg, "host", pipeline)
	var seen []string
	w.SetNotesIngester(func(_ context.Context, p string) error {
		seen = append(seen, p)
		return nil
	})
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	if len(seen) != 0 {
		t.Fatalf("seed must not emit; got %v", seen)
	}
	future := time.Now().Add(2 * time.Second)
	if err := os.Chtimes(notePath, future, future); err != nil {
		t.Fatalf("chtimes: %v", err)
	}
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(seen) != 1 || seen[0] != notePath {
		t.Fatalf("expected one ingest of %s, got %v", notePath, seen)
	}
}

func TestRunOnceCallsNotesIngesterOnNewNote(t *testing.T) {
	inbox := t.TempDir()
	root := t.TempDir()
	cfg := SphereConfig{Inbox: inbox, MeetingsRoot: root}
	pipeline := AudioPipelineFunc(func(context.Context, string) error { return nil })
	w := mustWatcher(t, cfg, "host", pipeline)
	var seen []string
	w.SetNotesIngester(func(_ context.Context, p string) error {
		seen = append(seen, p)
		return nil
	})
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("seed: %v", err)
	}
	notePath := filepath.Join(root, "2026-05-01-board", "MEETING_NOTES.md")
	mustWriteBytes(t, notePath, []byte("# Board\n"))
	if err := w.RunOnce(context.Background()); err != nil {
		t.Fatalf("second run: %v", err)
	}
	if len(seen) != 1 || seen[0] != notePath {
		t.Fatalf("expected new note ingest, got %v", seen)
	}
}

func TestSphereConfigClassifyHonoursShortMemoCutoff(t *testing.T) {
	cfg := SphereConfig{ShortMemoSeconds: 60}
	if got := cfg.Classify(45); got != MemoShort {
		t.Fatalf("45s should be short, got %d", got)
	}
	if got := cfg.Classify(60); got != MemoLong {
		t.Fatalf("60s should be long (boundary), got %d", got)
	}
	if got := cfg.Classify(0); got != MemoLong {
		t.Fatalf("zero duration should fall back to long, got %d", got)
	}
}

func mustWatcher(t *testing.T, cfg SphereConfig, host string, pipeline AudioPipeline) *Watcher {
	t.Helper()
	w, err := NewWatcher(cfg, host, pipeline, 0)
	if err != nil {
		t.Fatalf("NewWatcher: %v", err)
	}
	return w
}

func mustWriteBytes(t *testing.T, path string, body []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, body, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
