package meetingkickoff

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/zulip"
)

// ProviderFactory hands out a Zulip MessagesProvider for the given
// sphere config. The MCP server wires this to its tested factory; the
// tests in this package supply a fake.
type ProviderFactory func(meetings.ZulipConfig) (zulip.MessagesProvider, error)

// HandleArgs is the JSON-shaped argument map the MCP layer passes to
// Run. Keys map directly onto the `brain.meeting.kickoff` schema:
// `sphere`, `meeting_id`, `stream`, `topic`, `cutoff`, `window`,
// `questions`, `prior_note_path`, plus `sources_config`.
type HandleArgs map[string]interface{}

// Run resolves all kickoff inputs from args, fetches the Zulip topic
// via factory, reads the optional prior meeting note from the brain
// vault, and returns the assembled payload.
func Run(args HandleArgs, brainCfg *brain.Config, sourcesConfigPath string, sourcesExplicit bool, factory ProviderFactory) (map[string]interface{}, error) {
	sphere := strings.TrimSpace(stringArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if _, ok := brainCfg.Vault(brain.Sphere(sphere)); !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	meetingsCfg, err := meetings.Load(sourcesConfigPath, sourcesExplicit)
	if err != nil {
		return nil, err
	}
	sphereCfg, _ := meetingsCfg.Sphere(sphere)
	cutoff, err := ParseCutoff(stringArg(args, "cutoff"))
	if err != nil {
		return nil, err
	}
	window, err := ParseWindow(stringArg(args, "window"))
	if err != nil {
		return nil, err
	}
	stream, topic, meetingID, err := ResolveStreamAndTopic(stringArg(args, "stream"), stringArg(args, "topic"), stringArg(args, "meeting_id"), sphereCfg.Zulip, cutoff)
	if err != nil {
		return nil, err
	}
	if factory == nil {
		return nil, errors.New("zulip provider factory is not configured")
	}
	provider, err := factory(sphereCfg.Zulip)
	if err != nil {
		return nil, err
	}
	messages, err := provider.Messages(context.Background(), zulip.MessagesParams{
		Stream: stream,
		Topic:  topic,
		After:  cutoff.Add(-window),
		Before: cutoff,
		Limit:  DefaultMessageLimit,
	})
	if err != nil {
		return nil, err
	}
	priorNote, priorRel, err := loadPriorNote(stringArg(args, "prior_note_path"), brainCfg, sphere)
	if err != nil {
		return nil, err
	}
	result := Build(Request{
		Messages:       messages,
		Cutoff:         cutoff,
		Window:         window,
		FrameQuestions: stringListArg(args, "questions"),
		PriorNote:      priorNote,
	})
	return ToPayload(Envelope{
		Sphere:        sphere,
		MeetingID:     meetingID,
		Stream:        stream,
		Topic:         topic,
		Cutoff:        cutoff,
		Window:        window,
		PriorNotePath: priorRel,
	}, result), nil
}

func loadPriorNote(rel string, cfg *brain.Config, sphere string) (string, string, error) {
	clean := strings.TrimSpace(rel)
	if clean == "" {
		return "", "", nil
	}
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(sphere), clean)
	if err != nil {
		return "", "", err
	}
	data, err := os.ReadFile(resolved.Path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", nil
		}
		return "", "", err
	}
	return string(data), filepath.ToSlash(resolved.Rel), nil
}

func stringArg(args HandleArgs, key string) string {
	value, ok := args[key]
	if !ok {
		return ""
	}
	if s, ok := value.(string); ok {
		return s
	}
	return ""
}

func stringListArg(args HandleArgs, key string) []string {
	value, ok := args[key]
	if !ok {
		return nil
	}
	switch v := value.(type) {
	case []string:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if clean := strings.TrimSpace(item); clean != "" {
				out = append(out, clean)
			}
		}
		return out
	case []interface{}:
		out := make([]string, 0, len(v))
		for _, item := range v {
			if s, ok := item.(string); ok {
				if clean := strings.TrimSpace(s); clean != "" {
					out = append(out, clean)
				}
			}
		}
		return out
	}
	return nil
}
