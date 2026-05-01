package meetings

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestPipelineRoutesShortMemoToQuickPath(t *testing.T) {
	var quickCalled, longCalled bool
	pipeline := Pipeline{
		Cfg:        SphereConfig{ShortMemoSeconds: 60},
		Sphere:     "work",
		Probe:      func(context.Context, string) (int, error) { return 25, nil },
		Transcribe: func(context.Context, string) (string, error) { return "Send Ada the budget by Friday.", nil },
		QuickRender: func(_ context.Context, transcript string) (string, error) {
			if !strings.Contains(transcript, "budget") {
				t.Fatalf("transcript not propagated: %q", transcript)
			}
			return "send ada the budget by Friday", nil
		},
		LongRender: func(context.Context, string, string) (string, error) {
			longCalled = true
			return "ignored", nil
		},
		WriteQuick: func(_ context.Context, sphere, outcome, transcript, audio string) error {
			quickCalled = true
			if sphere != "work" || outcome == "" || audio == "" || transcript == "" {
				t.Fatalf("write args sphere=%q outcome=%q audio=%q transcript=%q", sphere, outcome, audio, transcript)
			}
			return nil
		},
		IngestMeeting: func(context.Context, string, string, string) (string, error) {
			t.Fatal("long branch must not run for short memo")
			return "", nil
		},
	}
	if err := pipeline.Process(context.Background(), "/tmp/memo-001.m4a"); err != nil {
		t.Fatalf("Process: %v", err)
	}
	if !quickCalled || longCalled {
		t.Fatalf("quick=%v long=%v", quickCalled, longCalled)
	}
}

func TestPipelineRoutesLongMemoToMeetingPath(t *testing.T) {
	now := time.Date(2026, 5, 1, 12, 0, 0, 0, time.UTC)
	pipeline := Pipeline{
		Cfg:        SphereConfig{ShortMemoSeconds: 60},
		Sphere:     "work",
		Probe:      func(context.Context, string) (int, error) { return 1800, nil },
		Transcribe: func(context.Context, string) (string, error) { return "long meeting transcript", nil },
		QuickRender: func(context.Context, string) (string, error) {
			t.Fatal("quick branch must not run for long memo")
			return "", nil
		},
		LongRender: func(_ context.Context, slug, transcript string) (string, error) {
			if slug == "" || transcript == "" {
				t.Fatalf("slug=%q transcript=%q", slug, transcript)
			}
			return "## Action Checklist\n\n### Ada\n- [ ] do the thing\n", nil
		},
		WriteQuick: func(context.Context, string, string, string, string) error {
			return errors.New("must not be called")
		},
		IngestMeeting: func(_ context.Context, sphere, slug, body string) (string, error) {
			if sphere != "work" || !strings.Contains(body, "Action Checklist") {
				t.Fatalf("ingest args sphere=%q body=%q", sphere, body)
			}
			if !strings.HasPrefix(slug, "2026-05-01-") {
				t.Fatalf("slug must start with date prefix, got %q", slug)
			}
			return "/tmp/meetings/" + slug + "/MEETING_NOTES.md", nil
		},
		NowFunc: func() time.Time { return now },
	}
	if err := pipeline.Process(context.Background(), "/tmp/standup.m4a"); err != nil {
		t.Fatalf("Process: %v", err)
	}
}

func TestPipelineEmptyTranscriptIsAFailure(t *testing.T) {
	pipeline := Pipeline{
		Cfg:           SphereConfig{ShortMemoSeconds: 60},
		Sphere:        "work",
		Probe:         func(context.Context, string) (int, error) { return 30, nil },
		Transcribe:    func(context.Context, string) (string, error) { return "   ", nil },
		QuickRender:   func(context.Context, string) (string, error) { return "x", nil },
		LongRender:    func(context.Context, string, string) (string, error) { return "x", nil },
		WriteQuick:    func(context.Context, string, string, string, string) error { return nil },
		IngestMeeting: func(context.Context, string, string, string) (string, error) { return "", nil },
	}
	if err := pipeline.Process(context.Background(), "/tmp/silent.m4a"); err == nil || !strings.Contains(err.Error(), "empty transcript") {
		t.Fatalf("expected empty-transcript error, got %v", err)
	}
}

func TestDefaultSlugFromAudioRespectsExistingDatePrefix(t *testing.T) {
	now := time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)
	if got := DefaultSlugFromAudio("/tmp/2026-04-29-board.m4a", now); got != "2026-04-29-board" {
		t.Fatalf("date-prefixed slug = %q", got)
	}
	if got := DefaultSlugFromAudio("/tmp/standup.m4a", now); got != "2026-05-01-standup" {
		t.Fatalf("non-prefixed slug = %q", got)
	}
}
