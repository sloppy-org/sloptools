package meetings

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// DurationProbe returns the duration of audioPath in whole seconds. The
// default implementation shells out to `ffprobe` and parses
// `format.duration`; tests inject deterministic stubs.
type DurationProbe func(ctx context.Context, audioPath string) (int, error)

// Transcriber runs an automatic-speech-recognition pipeline against the
// audio file and returns the recognised plain-text transcript.
type Transcriber func(ctx context.Context, audioPath string) (string, error)

// QuickRenderer turns a transcript into a single-line outcome suitable
// for a quick commitment (short memo path). Implementations should
// surgically extract the request, not summarise.
type QuickRenderer func(ctx context.Context, transcript string) (string, error)

// LongRenderer turns a transcript into a complete `MEETING_NOTES.md`
// body in the canonical template, ready for the existing parser. The
// caller supplies a slug derived from the meeting timestamp/topic so the
// renderer can include it in the heading.
type LongRenderer func(ctx context.Context, slug, transcript string) (string, error)

// QuickWriter persists the rendered short-memo outcome as a fresh
// commitment under the relevant sphere (typically by invoking
// brain.gtd.write).
type QuickWriter func(ctx context.Context, sphere, outcome, transcript, audioPath string) error

// LongIngester writes the rendered MEETING_NOTES.md to the configured
// meetings root and triggers brain.gtd.ingest for that path. The
// returned absolute path is the file that was created so the watcher
// can include it in any user-facing log.
type LongIngester func(ctx context.Context, sphere, slug, body string) (string, error)

// Pipeline is the production AudioPipeline used by the watcher. The
// individual steps are injected so tests can drive the pipeline without
// real ffprobe / whisper binaries.
type Pipeline struct {
	Cfg           SphereConfig
	Sphere        string
	Probe         DurationProbe
	Transcribe    Transcriber
	QuickRender   QuickRenderer
	LongRender    LongRenderer
	WriteQuick    QuickWriter
	IngestMeeting LongIngester
	NowFunc       func() time.Time
	SlugFromAudio func(audioPath string, now time.Time) string
}

// Process implements AudioPipeline by running the classify → transcribe
// → render → persist → ingest chain. Any error is wrapped with the
// failed step so the sidecar log is actionable.
func (p Pipeline) Process(ctx context.Context, audioPath string) error {
	if err := p.validate(); err != nil {
		return err
	}
	duration, err := p.Probe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("probe duration: %w", err)
	}
	transcript, err := p.Transcribe(ctx, audioPath)
	if err != nil {
		return fmt.Errorf("transcribe: %w", err)
	}
	transcript = strings.TrimSpace(transcript)
	if transcript == "" {
		return errors.New("transcribe: empty transcript")
	}
	switch p.Cfg.Classify(duration) {
	case MemoShort:
		outcome, err := p.QuickRender(ctx, transcript)
		if err != nil {
			return fmt.Errorf("render quick: %w", err)
		}
		if strings.TrimSpace(outcome) == "" {
			return errors.New("render quick: empty outcome")
		}
		return p.WriteQuick(ctx, p.Sphere, outcome, transcript, audioPath)
	default:
		now := p.now()
		slug := p.slugFor(audioPath, now)
		body, err := p.LongRender(ctx, slug, transcript)
		if err != nil {
			return fmt.Errorf("render meeting: %w", err)
		}
		if strings.TrimSpace(body) == "" {
			return errors.New("render meeting: empty body")
		}
		if _, err := p.IngestMeeting(ctx, p.Sphere, slug, body); err != nil {
			return fmt.Errorf("ingest meeting: %w", err)
		}
		return nil
	}
}

func (p Pipeline) validate() error {
	switch {
	case p.Probe == nil:
		return errors.New("pipeline: Probe is required")
	case p.Transcribe == nil:
		return errors.New("pipeline: Transcribe is required")
	case p.QuickRender == nil:
		return errors.New("pipeline: QuickRender is required")
	case p.LongRender == nil:
		return errors.New("pipeline: LongRender is required")
	case p.WriteQuick == nil:
		return errors.New("pipeline: WriteQuick is required")
	case p.IngestMeeting == nil:
		return errors.New("pipeline: IngestMeeting is required")
	case strings.TrimSpace(p.Sphere) == "":
		return errors.New("pipeline: Sphere is required")
	}
	return nil
}

func (p Pipeline) now() time.Time {
	if p.NowFunc != nil {
		return p.NowFunc()
	}
	return time.Now().UTC()
}

func (p Pipeline) slugFor(audioPath string, now time.Time) string {
	if p.SlugFromAudio != nil {
		return p.SlugFromAudio(audioPath, now)
	}
	return DefaultSlugFromAudio(audioPath, now)
}

// DefaultSlugFromAudio derives a meeting slug from the audio basename.
// Strips the extension, lower-cases and slugifies the result, prepending
// the date in YYYY-MM-DD form when the basename does not already start
// with one.
func DefaultSlugFromAudio(audioPath string, now time.Time) string {
	base := strings.TrimSuffix(filepath.Base(audioPath), filepath.Ext(audioPath))
	cleaned := slugifyMeetingBase(base)
	if cleaned == "" {
		cleaned = "memo"
	}
	if hasISODatePrefix(cleaned) {
		return cleaned
	}
	return now.Format("2006-01-02") + "-" + cleaned
}

func slugifyMeetingBase(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	last := byte(0)
	for i := 0; i < len(value); i++ {
		c := value[i]
		switch {
		case c >= 'a' && c <= 'z', c >= '0' && c <= '9':
			b.WriteByte(c)
			last = c
		case c == '-' || c == '_' || c == ' ' || c == '.':
			if last != '-' && b.Len() > 0 {
				b.WriteByte('-')
				last = '-'
			}
		}
	}
	return strings.Trim(b.String(), "-")
}

func hasISODatePrefix(value string) bool {
	if len(value) < len("2006-01-02") {
		return false
	}
	prefix := value[:10]
	if prefix[4] != '-' || prefix[7] != '-' {
		return false
	}
	for i, c := range prefix {
		if i == 4 || i == 7 {
			continue
		}
		if c < '0' || c > '9' {
			return false
		}
	}
	return true
}

// FFProbeDurationProbe returns a DurationProbe that shells out to ffprobe.
// The command is `ffprobe -v error -show_entries format=duration
// -of default=noprint_wrappers=1:nokey=1 <audio>`. Override with the
// configured TranscribeCommand[0] when the user prefers a different
// binary by passing its path here. Output is parsed as a float-second
// value and rounded down to int seconds.
func FFProbeDurationProbe(binary string) DurationProbe {
	if strings.TrimSpace(binary) == "" {
		binary = "ffprobe"
	}
	return func(ctx context.Context, audioPath string) (int, error) {
		cmd := exec.CommandContext(ctx, binary, "-v", "error", "-show_entries", "format=duration", "-of", "default=noprint_wrappers=1:nokey=1", audioPath)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return 0, fmt.Errorf("%s: %s", binary, msg)
			}
			return 0, err
		}
		seconds, err := strconv.ParseFloat(strings.TrimSpace(stdout.String()), 64)
		if err != nil {
			return 0, fmt.Errorf("%s: parse duration %q: %w", binary, stdout.String(), err)
		}
		if seconds < 0 {
			seconds = 0
		}
		return int(seconds), nil
	}
}

// CommandTranscriber runs a configurable command-line transcriber. The
// last argument is replaced with the audio path so callers can compose
// `whisper-cli --model base --output-format txt` and have the watcher
// fill in the file at the end. The transcript is read from stdout.
func CommandTranscriber(command []string) Transcriber {
	return func(ctx context.Context, audioPath string) (string, error) {
		if len(command) == 0 {
			return "", errors.New("transcribe command not configured")
		}
		args := append([]string(nil), command[1:]...)
		args = append(args, audioPath)
		cmd := exec.CommandContext(ctx, command[0], args...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return "", fmt.Errorf("%s: %s", command[0], msg)
			}
			return "", err
		}
		return strings.TrimSpace(stdout.String()), nil
	}
}

// CommandRenderer runs a configurable command that consumes the
// transcript on stdin and writes the rendered artefact (a one-line
// outcome for quick memos, a full MEETING_NOTES.md body for long memos)
// to stdout. extraEnv lets callers pass per-mode markers, e.g.
// MEMO_KIND=quick or MEMO_SLUG=<slug>, so a single binary can switch
// behaviour without per-mode wrappers.
func CommandRenderer(command []string, extraEnv map[string]string) func(ctx context.Context, transcript string) (string, error) {
	return func(ctx context.Context, transcript string) (string, error) {
		if len(command) == 0 {
			return "", errors.New("render command not configured")
		}
		cmd := exec.CommandContext(ctx, command[0], command[1:]...)
		cmd.Stdin = strings.NewReader(transcript)
		cmd.Env = append(os.Environ(), envEntries(extraEnv)...)
		var stdout, stderr bytes.Buffer
		cmd.Stdout = &stdout
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg != "" {
				return "", fmt.Errorf("%s: %s", command[0], msg)
			}
			return "", err
		}
		return strings.TrimSpace(stdout.String()), nil
	}
}

func envEntries(values map[string]string) []string {
	out := make([]string, 0, len(values))
	for key, value := range values {
		out = append(out, key+"="+value)
	}
	return out
}
