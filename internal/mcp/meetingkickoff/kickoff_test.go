package meetingkickoff

import (
	"reflect"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/zulip"
)

var fixtureCutoff = time.Date(2026, 5, 4, 9, 0, 0, 0, time.UTC)

func msg(sender, body string, offset time.Duration) zulip.Message {
	return zulip.Message{
		SenderName: sender,
		Topic:      "2026-05-04 sync",
		Stream:     "plasma-orga",
		Timestamp:  fixtureCutoff.Add(offset),
		Content:    body,
	}
}

func TestBuildEmptyTopicReturnsFrameOnlyAndNoBreakouts(t *testing.T) {
	got := Build(Request{
		Messages:       nil,
		Cutoff:         fixtureCutoff,
		FrameQuestions: []string{"What blocks the next code drop?"},
		PriorNote: `# 2026-04-27 plasma orga

## Decisions

- Adopt the new netidee scope.
- [x] Move the kickoff to Tuesday.
`,
	})
	if len(got.Breakouts) != 0 {
		t.Fatalf("breakouts = %#v, want none", got.Breakouts)
	}
	if len(got.PairOffCycle) != 0 {
		t.Fatalf("pair-off-cycle = %#v, want none", got.PairOffCycle)
	}
	if len(got.Posts) != 0 {
		t.Fatalf("posts = %#v, want none", got.Posts)
	}
	wantQuestions := []string{"What blocks the next code drop?"}
	if !reflect.DeepEqual(got.Frame.Questions, wantQuestions) {
		t.Fatalf("questions = %#v, want %#v", got.Frame.Questions, wantQuestions)
	}
	wantDecisions := []string{"Adopt the new netidee scope.", "Move the kickoff to Tuesday."}
	if !reflect.DeepEqual(got.Frame.Decisions, wantDecisions) {
		t.Fatalf("decisions = %#v, want %#v", got.Frame.Decisions, wantDecisions)
	}
}

func TestBuildSinglePosterRoutesToPairOffCycle(t *testing.T) {
	got := Build(Request{
		Messages: []zulip.Message{
			msg("Ada Example", "I want to sync with @**Bo Coder** about NetIdee budget numbers.", -2*time.Hour),
		},
		Cutoff: fixtureCutoff,
	})
	if len(got.Breakouts) != 0 {
		t.Fatalf("breakouts = %#v, want zero (single poster)", got.Breakouts)
	}
	if len(got.PairOffCycle) != 1 {
		t.Fatalf("pair-off-cycle = %#v, want 1", got.PairOffCycle)
	}
	pair := got.PairOffCycle[0]
	if pair.Poster != "Ada Example" {
		t.Fatalf("poster = %q, want Ada Example", pair.Poster)
	}
	if !reflect.DeepEqual(pair.With, []string{"Bo Coder"}) {
		t.Fatalf("with = %#v, want [Bo Coder]", pair.With)
	}
}

func TestBuildClustersBySharedMentions(t *testing.T) {
	got := Build(Request{
		Messages: []zulip.Message{
			msg("Ada Example", "Sync with @**Bo Coder** on grant numbers.", -3*time.Hour),
			msg("Bo Coder", "Need to align with @**Ada Example** before review.", -2*time.Hour),
			msg("Cy Reviewer", "Want to talk to @**Ada Example** about plot scaling.", -1*time.Hour),
		},
		Cutoff: fixtureCutoff,
	})
	if len(got.Breakouts) != 1 {
		t.Fatalf("breakouts = %#v, want 1", got.Breakouts)
	}
	breakout := got.Breakouts[0]
	wantParticipants := []string{"Ada Example", "Bo Coder", "Cy Reviewer"}
	if !reflect.DeepEqual(breakout.Participants, wantParticipants) {
		t.Fatalf("participants = %#v, want %#v", breakout.Participants, wantParticipants)
	}
	if len(breakout.Posters) != 3 {
		t.Fatalf("posters = %#v, want 3", breakout.Posters)
	}
	if len(got.PairOffCycle) != 0 {
		t.Fatalf("pair-off-cycle = %#v, want none", got.PairOffCycle)
	}
}

func TestBuildOverflowMovesExtraGroupsToPairOffCycle(t *testing.T) {
	messages := []zulip.Message{
		msg("Ada Example", "Sync with @**Ann Helper** on alpha.", -8*time.Hour),
		msg("Ann Helper", "Pair with @**Ada Example** on alpha.", -7*time.Hour),

		msg("Bea Bot", "Sync with @**Bo Coder** on beta.", -6*time.Hour),
		msg("Bo Coder", "Pair with @**Bea Bot** on beta.", -5*time.Hour),

		msg("Cara Coder", "Sync with @**Cy Reviewer** on gamma.", -4*time.Hour),
		msg("Cy Reviewer", "Pair with @**Cara Coder** on gamma.", -3*time.Hour),

		msg("Dan Dev", "Sync with @**Di Tester** on delta.", -2*time.Hour),
		msg("Di Tester", "Pair with @**Dan Dev** on delta.", time.Duration(-2*time.Hour-30*time.Minute)),

		msg("Ed Eng", "Sync with @**Eli Eng** on epsilon.", -1*time.Hour),
		msg("Eli Eng", "Pair with @**Ed Eng** on epsilon.", time.Duration(-30*time.Minute)),
	}
	got := Build(Request{Messages: messages, Cutoff: fixtureCutoff})
	if len(got.Breakouts) != MaxBreakouts {
		t.Fatalf("breakouts = %d, want %d", len(got.Breakouts), MaxBreakouts)
	}
	if len(got.PairOffCycle) == 0 {
		t.Fatalf("pair-off-cycle = %#v, want overflow entries", got.PairOffCycle)
	}
	posters := map[string]struct{}{}
	for _, breakout := range got.Breakouts {
		for _, p := range breakout.Posters {
			posters[canonicalName(p)] = struct{}{}
		}
	}
	overflowSeen := map[string]struct{}{}
	for _, pair := range got.PairOffCycle {
		overflowSeen[canonicalName(pair.Poster)] = struct{}{}
	}
	for poster := range posters {
		if _, ok := overflowSeen[poster]; ok {
			t.Fatalf("poster %q appears in both breakouts and pair-off-cycle", poster)
		}
	}
}

func TestBuildDropsMessagesOutsideTheLookBackWindow(t *testing.T) {
	got := Build(Request{
		Messages: []zulip.Message{
			msg("Old Ola", "from last week", -48*time.Hour),
			msg("Ada Example", "@**Bo Coder** budget", -2*time.Hour),
			msg("Bo Coder", "@**Ada Example** budget", -time.Hour),
			msg("Future Fae", "after the meeting", time.Hour),
		},
		Cutoff: fixtureCutoff,
	})
	if len(got.Posts) != 2 {
		t.Fatalf("posts = %#v, want 2 inside window", got.Posts)
	}
	for _, post := range got.Posts {
		if post.Sender == "Old Ola" || post.Sender == "Future Fae" {
			t.Fatalf("out-of-window post leaked: %#v", post)
		}
	}
}

func TestBuildFrameTrimsToMaxQuestions(t *testing.T) {
	got := BuildFrame([]string{"q1", "  ", "q2", "q3"}, "")
	if !reflect.DeepEqual(got.Questions, []string{"q1", "q2"}) {
		t.Fatalf("questions = %#v, want [q1 q2]", got.Questions)
	}
}
