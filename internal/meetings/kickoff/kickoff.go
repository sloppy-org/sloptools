package kickoff

import (
	"sort"
	"time"

	"github.com/sloppy-org/sloptools/internal/zulip"
)

// Result is the full payload returned by `brain.meeting.kickoff`. It
// contains the draft frame plus the breakouts and pair-off-cycle list
// derived from the pre-meeting Zulip posts.
type Result struct {
	Frame        Frame
	Breakouts    []Breakout
	PairOffCycle []PairOffCycle
	Posts        []Post
}

// Request bundles the inputs Build needs. Cutoff is the meeting start
// time; Window is the look-back duration (defaults to 24h per §5).
// PriorNote is the raw markdown of the previous meeting note (may be
// empty when the meeting is the first in the series).
type Request struct {
	Messages       []zulip.Message
	Cutoff         time.Time
	Window         time.Duration
	FrameQuestions []string
	PriorNote      string
}

// DefaultWindow is the §5 24h look-back window for pre-meeting posts.
const DefaultWindow = 24 * time.Hour

// Build runs the full pipeline: filter Zulip messages to the look-back
// window, parse posts, cluster into breakouts, drop singletons and
// overflow into pair-off-cycle, and assemble the Frame from caller
// questions plus prior-note decisions.
func Build(req Request) Result {
	window := req.Window
	if window <= 0 {
		window = DefaultWindow
	}
	filtered := filterByWindow(req.Messages, req.Cutoff, window)
	posts := ParsePosts(filtered)
	cluster := Cluster(posts)
	frame := BuildFrame(req.FrameQuestions, req.PriorNote)
	return Result{
		Frame:        frame,
		Breakouts:    cluster.Breakouts,
		PairOffCycle: cluster.PairOffCycle,
		Posts:        posts,
	}
}

func filterByWindow(messages []zulip.Message, cutoff time.Time, window time.Duration) []zulip.Message {
	if cutoff.IsZero() {
		return append([]zulip.Message(nil), messages...)
	}
	after := cutoff.Add(-window)
	out := make([]zulip.Message, 0, len(messages))
	for _, msg := range messages {
		if msg.Timestamp.IsZero() {
			continue
		}
		if msg.Timestamp.Before(after) {
			continue
		}
		if !msg.Timestamp.Before(cutoff) {
			continue
		}
		out = append(out, msg)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Timestamp.Before(out[j].Timestamp) })
	return out
}
