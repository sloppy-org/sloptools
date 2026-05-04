package meetingkickoff

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/meetings"
)

// DatePlaceholder is the literal token that ZulipConfig.TopicFormat
// replaces with the cutoff date in YYYY-MM-DD form.
const DatePlaceholder = "{date}"

// DefaultMessageLimit caps the number of messages the kickoff helper
// asks the Zulip provider for. The §5 pre-meeting topic carries one
// post per participant, so 200 is a generous ceiling.
const DefaultMessageLimit = 200

// ParseCutoff accepts RFC3339, YYYY-MM-DDTHH:MM, or YYYY-MM-DD and
// returns a UTC time. An empty input falls back to time.Now in UTC.
func ParseCutoff(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Now().UTC(), nil
	}
	if t, err := time.Parse(time.RFC3339, clean); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02T15:04", clean); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse("2006-01-02", clean); err == nil {
		return t.UTC(), nil
	}
	return time.Time{}, fmt.Errorf("cutoff must be RFC3339, YYYY-MM-DDTHH:MM, or YYYY-MM-DD: %q", raw)
}

// ParseWindow accepts a Go duration string and returns the look-back
// window. An empty input falls back to DefaultWindow.
func ParseWindow(raw string) (time.Duration, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return DefaultWindow, nil
	}
	dur, err := time.ParseDuration(clean)
	if err != nil {
		return 0, fmt.Errorf("window must be a Go duration (e.g. 24h): %q", raw)
	}
	if dur <= 0 {
		return 0, fmt.Errorf("window must be positive: %q", raw)
	}
	return dur, nil
}

// ResolveStreamAndTopic picks the Zulip stream and topic for a kickoff
// request. Explicit `stream`/`topic` arguments win; otherwise the
// `meeting_id` argument is looked up in cfg.MeetingSeries and topics
// are rendered from the configured TopicFormat with the cutoff date.
func ResolveStreamAndTopic(stream, topic, meetingID string, cfg meetings.ZulipConfig, cutoff time.Time) (string, string, string, error) {
	stream = strings.TrimSpace(stream)
	topic = strings.TrimSpace(topic)
	meetingID = strings.TrimSpace(meetingID)
	if stream == "" && meetingID != "" {
		series, ok := cfg.SeriesStream(meetingID)
		if !ok {
			return "", "", "", fmt.Errorf("meeting_id %q is not configured under [meetings.<sphere>.meeting_series]", meetingID)
		}
		stream = series.Stream
		if topic == "" {
			topic = RenderTopic(series.TopicFormat, cutoff)
		}
	}
	if topic == "" {
		topic = RenderTopic(cfg.TopicFormat, cutoff)
	}
	if stream == "" {
		return "", "", "", errors.New("stream is required (set 'stream' arg or configure 'meeting_id' under meeting_series)")
	}
	if topic == "" {
		return "", "", "", errors.New("topic is required (set 'topic' arg or configure topic_format)")
	}
	return stream, topic, meetingID, nil
}

// RenderTopic substitutes the cutoff date into format. An empty format
// returns the empty string so the caller can fall back to a different
// source.
func RenderTopic(format string, cutoff time.Time) string {
	clean := strings.TrimSpace(format)
	if clean == "" {
		return ""
	}
	date := cutoff.UTC().Format("2006-01-02")
	return strings.ReplaceAll(clean, DatePlaceholder, date)
}
