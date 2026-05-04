package meetingkickoff

import (
	"time"
)

// Envelope captures the per-call inputs the MCP layer needs to echo
// back alongside the build result (so callers can confirm which
// stream/topic the response describes).
type Envelope struct {
	Sphere        string
	MeetingID     string
	Stream        string
	Topic         string
	Cutoff        time.Time
	Window        time.Duration
	PriorNotePath string
}

// ToPayload converts a Result and its Envelope into the JSON-friendly
// map the MCP server returns. Empty slices are returned as empty
// (not nil) so consumers do not see `null` for missing buckets.
func ToPayload(env Envelope, result Result) map[string]interface{} {
	return map[string]interface{}{
		"sphere":          env.Sphere,
		"meeting_id":      env.MeetingID,
		"stream":          env.Stream,
		"topic":           env.Topic,
		"cutoff":          env.Cutoff.Format(time.RFC3339),
		"window_seconds":  int64(env.Window / time.Second),
		"frame":           frameToMap(result.Frame),
		"breakouts":       breakoutsToMap(result.Breakouts),
		"pair_off_cycle":  pairOffCycleToMap(result.PairOffCycle),
		"posts":           postsToMap(result.Posts),
		"prior_note_path": env.PriorNotePath,
	}
}

func frameToMap(frame Frame) map[string]interface{} {
	return map[string]interface{}{
		"questions": ensureStrings(frame.Questions),
		"decisions": ensureStrings(frame.Decisions),
	}
}

func breakoutsToMap(items []Breakout) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"participants": ensureStrings(item.Participants),
			"posters":      ensureStrings(item.Posters),
			"posts":        postsToMap(item.Posts),
		})
	}
	return out
}

func pairOffCycleToMap(items []PairOffCycle) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"poster": item.Poster,
			"with":   ensureStrings(item.With),
			"body":   item.Body,
		})
	}
	return out
}

func postsToMap(items []Post) []map[string]interface{} {
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		out = append(out, map[string]interface{}{
			"sender":   item.Sender,
			"mentions": ensureStrings(item.Mentions),
			"body":     item.Body,
		})
	}
	return out
}

func ensureStrings(values []string) []string {
	if values == nil {
		return []string{}
	}
	return values
}
