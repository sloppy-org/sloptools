// Package meetings — watcher.go implements the canonical-host voice-memo
// pipeline described in issue #56. A polling loop watches the configured
// `inbox` directory; every new audio file is classified, transcribed,
// and either written as a quick-commitment (short memo) or rendered into
// a `MEETING_NOTES.md` and re-ingested via brain.gtd.ingest (long memo).
// Successful processing deletes the source audio; failures leave the
// audio in place and write a `<basename>.failed` sidecar with the error
// chain for human triage.
package meetings

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

// AudioPipeline is the per-file work the watcher dispatches once it has
// found a new candidate. Implementations are responsible for transcript
// generation, classification, artefact writing, and re-ingest. The
// watcher cleans up after a successful return; on error it leaves the
// audio in place and writes the sidecar.
type AudioPipeline interface {
	Process(ctx context.Context, audioPath string) error
}

// AudioPipelineFunc adapts a function value to AudioPipeline.
type AudioPipelineFunc func(ctx context.Context, audioPath string) error

// Process implements AudioPipeline.
func (f AudioPipelineFunc) Process(ctx context.Context, audioPath string) error {
	return f(ctx, audioPath)
}

// NotesIngester re-ingests a single meeting notes file after the watcher
// detects an mtime change. Implementations typically wrap
// `brain.gtd.ingest --source meetings`. Errors are recorded by the
// watcher but do not stop the polling loop.
type NotesIngester func(ctx context.Context, notePath string) error

// Watcher polls a configured INBOX folder and routes audio files to a
// pipeline. When NotesIngester is set the watcher additionally tracks
// `MEETING_NOTES.md` files (and loose `<slug>.md` files) under
// cfg.MeetingsRoot. On startup the watcher walks every existing note
// and treats it like a new file (per issue #56 backfill contract,
// idempotent because re-ingest stamps stable IDs). On subsequent scans
// it re-ingests any file whose mtime moved or that newly appeared. The
// struct is safe to use from a single goroutine; concurrent callers
// must serialise themselves.
type Watcher struct {
	cfg         SphereConfig
	hostname    string
	pipeline    AudioPipeline
	notesIngest NotesIngester
	notesMtime  map[string]time.Time
	clock       func() time.Time
	interval    time.Duration
}

// FailedSidecarSuffix is appended to the audio filename when processing
// fails so subsequent watcher passes know to skip the file.
const FailedSidecarSuffix = ".failed"

// DefaultWatcherInterval is the polling cadence used when the caller
// passes a non-positive interval.
const DefaultWatcherInterval = 3 * time.Second

// audioExtensions lists the file types accepted by the pipeline. Lower-case.
var audioExtensions = map[string]bool{
	".m4a": true, ".mp3": true, ".wav": true, ".flac": true,
	".ogg": true, ".webm": true, ".aac": true, ".opus": true,
}

// NewWatcher validates the canonical-host contract and returns a ready
// Watcher. cfg must declare an Inbox path; CanonicalHost is enforced
// when non-empty by comparing hostname (case-insensitive). The pipeline
// is mandatory; interval defaults to DefaultWatcherInterval when ≤ 0.
func NewWatcher(cfg SphereConfig, hostname string, pipeline AudioPipeline, interval time.Duration) (*Watcher, error) {
	if strings.TrimSpace(cfg.Inbox) == "" {
		return nil, errors.New("watcher: inbox is required")
	}
	if pipeline == nil {
		return nil, errors.New("watcher: pipeline is required")
	}
	if cfg.CanonicalHost != "" && !strings.EqualFold(cfg.CanonicalHost, hostname) {
		return nil, &CanonicalHostError{Host: hostname, Wanted: cfg.CanonicalHost}
	}
	if interval <= 0 {
		interval = DefaultWatcherInterval
	}
	return &Watcher{
		cfg:        cfg,
		hostname:   hostname,
		pipeline:   pipeline,
		notesMtime: map[string]time.Time{},
		clock:      time.Now,
		interval:   interval,
	}, nil
}

// SetNotesIngester installs the callback the watcher invokes for every
// meeting note found under cfg.MeetingsRoot. The first scan after
// installation walks every existing `MEETING_NOTES.md` (or loose
// `<slug>.md`) and fires the callback once per note so historical
// material is backfilled per the issue #56 acceptance contract.
// Subsequent scans only re-fire when mtime advances or a new note
// appears. Passing nil disables notes watching.
func (w *Watcher) SetNotesIngester(ingest NotesIngester) {
	w.notesIngest = ingest
	w.notesMtime = map[string]time.Time{}
}

// CanonicalHostError is returned when NewWatcher refuses to start
// because the host is not the configured canonical processor.
type CanonicalHostError struct {
	Host   string
	Wanted string
}

func (e *CanonicalHostError) Error() string {
	return fmt.Sprintf("watcher refuses to start: host %q is not the canonical processor %q", e.Host, e.Wanted)
}

// RunOnce processes every audio file currently sitting in INBOX, then
// returns. It is the unit of work the polling loop schedules and the
// natural entry-point for tests. The first error from a per-file
// pipeline failure is logged via the sidecar but RunOnce keeps draining
// remaining files; the returned error is the last underlying error
// encountered while walking the directory itself. When a NotesIngester
// is installed the same call also detects mtime changes under
// cfg.MeetingsRoot and re-ingests the changed paths.
func (w *Watcher) RunOnce(ctx context.Context) error {
	files, err := w.scan()
	if err != nil {
		return err
	}
	for _, audio := range files {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		w.processOne(ctx, audio)
	}
	if err := w.scanMeetingNotes(ctx); err != nil {
		return err
	}
	return nil
}

func (w *Watcher) scanMeetingNotes(ctx context.Context) error {
	if w.notesIngest == nil || strings.TrimSpace(w.cfg.MeetingsRoot) == "" {
		return nil
	}
	discovered, err := Discover(w.cfg.MeetingsRoot)
	if err != nil {
		return err
	}
	live := make(map[string]struct{}, len(discovered.Paths))
	for _, path := range discovered.Paths {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		live[path] = struct{}{}
		info, err := os.Stat(path)
		if err != nil {
			if os.IsNotExist(err) {
				continue
			}
			return err
		}
		modified := info.ModTime()
		previous, known := w.notesMtime[path]
		w.notesMtime[path] = modified
		if known && !modified.After(previous) {
			continue
		}
		if err := w.notesIngest(ctx, path); err != nil {
			w.writeSidecar(path, fmt.Errorf("ingest meeting notes: %w", err))
		}
	}
	for path := range w.notesMtime {
		if _, ok := live[path]; !ok {
			delete(w.notesMtime, path)
		}
	}
	return nil
}

// Run drives RunOnce in a loop until ctx is cancelled. Per-file errors
// are recorded as `.failed` sidecars; transient scan errors propagate
// out so the caller can decide whether to keep retrying.
func (w *Watcher) Run(ctx context.Context) error {
	ticker := time.NewTicker(w.interval)
	defer ticker.Stop()
	for {
		if err := w.RunOnce(ctx); err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func (w *Watcher) processOne(ctx context.Context, path string) {
	if err := w.pipeline.Process(ctx, path); err != nil {
		w.writeSidecar(path, err)
		return
	}
	if removeErr := os.Remove(path); removeErr != nil && !os.IsNotExist(removeErr) {
		w.writeSidecar(path, fmt.Errorf("delete after success: %w", removeErr))
	}
}

func (w *Watcher) writeSidecar(path string, cause error) {
	sidecar := path + FailedSidecarSuffix
	body := fmt.Sprintf("%s\t%s\t%s\n", w.clock().UTC().Format(time.RFC3339), filepath.Base(path), cause.Error())
	_ = os.WriteFile(sidecar, []byte(body), 0o644)
}

// scan returns the audio files in cfg.Inbox in deterministic order,
// excluding sidecars and other non-audio files.
func (w *Watcher) scan() ([]string, error) {
	entries, err := os.ReadDir(w.cfg.Inbox)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	var out []string
	skip := buildSidecarSkipSet(entries)
	for _, entry := range entries {
		if entry.IsDir() || !isAudioName(entry.Name()) {
			continue
		}
		if skip[entry.Name()] {
			continue
		}
		out = append(out, filepath.Join(w.cfg.Inbox, entry.Name()))
	}
	sort.Strings(out)
	return out, nil
}

func buildSidecarSkipSet(entries []fs.DirEntry) map[string]bool {
	out := map[string]bool{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		name := entry.Name()
		if !strings.HasSuffix(name, FailedSidecarSuffix) {
			continue
		}
		out[strings.TrimSuffix(name, FailedSidecarSuffix)] = true
	}
	return out
}

func isAudioName(name string) bool {
	return audioExtensions[strings.ToLower(filepath.Ext(name))]
}

// MemoClassification splits a memo into the short/long branches per
// SphereConfig.ShortMemoSeconds. duration ≤ 0 conservatively counts as
// long so an unparseable duration does not silently drop into the
// quick-commitment path.
type MemoClassification int

const (
	MemoShort MemoClassification = iota + 1
	MemoLong
)

// Classify returns the short/long branch for an audio file given its
// duration. The cutoff is inclusive on the short side: duration < cutoff
// is short; duration == cutoff is long. ShortMemoSeconds==0 falls back
// to DefaultShortMemoSeconds via SphereConfig.normalize.
func (c SphereConfig) Classify(durationSeconds int) MemoClassification {
	cutoff := c.ShortMemoSeconds
	if cutoff <= 0 {
		cutoff = DefaultShortMemoSeconds
	}
	if durationSeconds <= 0 {
		return MemoLong
	}
	if durationSeconds < cutoff {
		return MemoShort
	}
	return MemoLong
}
