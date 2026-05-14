// Package chat defines provider-neutral read access to team chat systems.
package chat

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"github.com/sloppy-org/sloptools/internal/meetings"
	"github.com/sloppy-org/sloptools/internal/zulip"
)

const ProviderZulip = "zulip"

type Config struct {
	bySphere map[string]SphereConfig
}

type SphereConfig struct {
	Sphere   string
	Provider string
	Zulip    ZulipConfig
}

type ZulipConfig struct {
	BaseURL string
	Email   string
	APIKey  string
}

type rawFile struct {
	Chat map[string]rawSphereConfig `toml:"chat"`
}

type rawSphereConfig struct {
	Provider string         `toml:"provider"`
	Zulip    rawZulipConfig `toml:"zulip"`
}

type rawZulipConfig struct {
	BaseURL string `toml:"base_url"`
	Email   string `toml:"email"`
	APIKey  string `toml:"api_key"`
}

type Handler struct {
	ConfigPath string
	Explicit   bool
	NewZulip   func(ZulipConfig) (ZulipProvider, error)
}

type ZulipProvider interface {
	Messages(context.Context, zulip.MessagesParams) ([]zulip.Message, error)
	Search(context.Context, zulip.SearchParams) ([]zulip.Message, error)
	Streams(context.Context) ([]zulip.Stream, error)
}

func Load(path string, explicit bool) (Config, error) {
	clean := strings.TrimSpace(path)
	if clean == "" {
		return Config{bySphere: map[string]SphereConfig{}}, nil
	}
	var raw rawFile
	if _, err := toml.DecodeFile(clean, &raw); err != nil {
		if !explicit && os.IsNotExist(err) {
			return Config{bySphere: map[string]SphereConfig{}}, nil
		}
		return Config{}, fmt.Errorf("load chat config %s: %w", clean, err)
	}
	out := Config{bySphere: map[string]SphereConfig{}}
	for sphere, entry := range raw.Chat {
		key := strings.ToLower(strings.TrimSpace(sphere))
		if key == "" {
			continue
		}
		provider := strings.ToLower(strings.TrimSpace(entry.Provider))
		if provider == "" {
			provider = ProviderZulip
		}
		out.bySphere[key] = SphereConfig{
			Sphere:   key,
			Provider: provider,
			Zulip: ZulipConfig{
				BaseURL: strings.TrimRight(strings.TrimSpace(entry.Zulip.BaseURL), "/"),
				Email:   strings.TrimSpace(entry.Zulip.Email),
				APIKey:  strings.TrimSpace(entry.Zulip.APIKey),
			},
		}
	}
	return out, nil
}

func (c Config) Sphere(sphere string) (SphereConfig, bool) {
	if c.bySphere == nil {
		return SphereConfig{}, false
	}
	cfg, ok := c.bySphere[strings.ToLower(strings.TrimSpace(sphere))]
	return cfg, ok
}

func (h Handler) Call(ctx context.Context, args map[string]interface{}) (map[string]interface{}, error) {
	action := strings.TrimSpace(stringArg(args, "action"))
	if action == "" {
		return nil, errors.New("action is required")
	}
	sphere := strings.TrimSpace(stringArg(args, "sphere"))
	if sphere == "" {
		sphere = "work"
	}
	cfg, err := h.sphereConfig(sphere)
	if err != nil {
		return nil, err
	}
	switch action {
	case "provider_list":
		return map[string]interface{}{"sphere": sphere, "providers": []string{cfg.Provider}, "count": 1}, nil
	case "stream_list":
		provider, err := h.zulipProvider(cfg)
		if err != nil {
			return nil, err
		}
		streams, err := provider.Streams(ctx)
		if err != nil {
			return nil, err
		}
		return map[string]interface{}{"sphere": sphere, "provider": ProviderZulip, "streams": streams, "count": len(streams)}, nil
	case "message_list":
		return h.messageList(ctx, cfg, args)
	case "message_search":
		return h.messageSearch(ctx, cfg, args)
	default:
		return nil, fmt.Errorf("sloppy_chat: unknown action %q", action)
	}
}

func (h Handler) messageList(ctx context.Context, cfg SphereConfig, args map[string]interface{}) (map[string]interface{}, error) {
	provider, err := h.zulipProvider(cfg)
	if err != nil {
		return nil, err
	}
	params, err := messageParams(args)
	if err != nil {
		return nil, err
	}
	messages, err := provider.Messages(ctx, params)
	if err != nil {
		return nil, err
	}
	return chatMessagesPayload(cfg.Sphere, messages, params.Stream, params.Topic, "", params.Limit), nil
}

func (h Handler) messageSearch(ctx context.Context, cfg SphereConfig, args map[string]interface{}) (map[string]interface{}, error) {
	provider, err := h.zulipProvider(cfg)
	if err != nil {
		return nil, err
	}
	params, err := searchParams(args)
	if err != nil {
		return nil, err
	}
	messages, err := provider.Search(ctx, params)
	if err != nil {
		return nil, err
	}
	return chatMessagesPayload(cfg.Sphere, messages, params.Stream, params.Topic, params.Query, params.Limit), nil
}

func (h Handler) sphereConfig(sphere string) (SphereConfig, error) {
	cfg, err := Load(h.ConfigPath, h.Explicit)
	if err != nil {
		return SphereConfig{}, err
	}
	if sc, ok := cfg.Sphere(sphere); ok {
		return sc, nil
	}
	meetingsCfg, err := meetings.Load(h.ConfigPath, h.Explicit)
	if err != nil {
		return SphereConfig{}, err
	}
	if mc, ok := meetingsCfg.Sphere(sphere); ok && strings.TrimSpace(mc.Zulip.BaseURL) != "" {
		return SphereConfig{
			Sphere:   sphere,
			Provider: ProviderZulip,
			Zulip: ZulipConfig{
				BaseURL: mc.Zulip.BaseURL,
				Email:   mc.Zulip.Email,
				APIKey:  mc.Zulip.APIKey,
			},
		}, nil
	}
	return SphereConfig{}, fmt.Errorf("chat provider is not configured for sphere %q in %s", sphere, h.ConfigPath)
}

func (h Handler) zulipProvider(cfg SphereConfig) (ZulipProvider, error) {
	if cfg.Provider != "" && cfg.Provider != ProviderZulip {
		return nil, fmt.Errorf("unsupported chat provider %q", cfg.Provider)
	}
	if h.NewZulip != nil {
		return h.NewZulip(cfg.Zulip)
	}
	return zulip.NewClient(zulip.Config{BaseURL: cfg.Zulip.BaseURL, Email: cfg.Zulip.Email, APIKey: cfg.Zulip.APIKey})
}

func ResolveConfigPath(path string) (string, bool, error) {
	clean := strings.TrimSpace(path)
	if clean != "" {
		if strings.HasPrefix(clean, "~/") {
			home, err := os.UserHomeDir()
			if err != nil {
				return "", true, err
			}
			clean = filepath.Join(home, strings.TrimPrefix(clean, "~/"))
		}
		return filepath.Clean(clean), true, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", false, err
	}
	return filepath.Join(home, ".config", "sloptools", "sources.toml"), false, nil
}

func messageParams(args map[string]interface{}) (zulip.MessagesParams, error) {
	after, err := parseOptionalTime(stringArg(args, "after"))
	if err != nil {
		return zulip.MessagesParams{}, err
	}
	before, err := parseOptionalTime(stringArg(args, "before"))
	if err != nil {
		return zulip.MessagesParams{}, err
	}
	return zulip.MessagesParams{
		Stream: stringArg(args, "stream"),
		Topic:  stringArg(args, "topic"),
		After:  after,
		Before: before,
		Limit:  boundedLimit(args, 50, 200),
	}, nil
}

func searchParams(args map[string]interface{}) (zulip.SearchParams, error) {
	after, err := parseOptionalTime(stringArg(args, "after"))
	if err != nil {
		return zulip.SearchParams{}, err
	}
	before, err := parseOptionalTime(stringArg(args, "before"))
	if err != nil {
		return zulip.SearchParams{}, err
	}
	return zulip.SearchParams{
		Query:  stringArg(args, "query"),
		Stream: stringArg(args, "stream"),
		Topic:  stringArg(args, "topic"),
		After:  after,
		Before: before,
		Limit:  boundedLimit(args, 50, 200),
	}, nil
}

func parseOptionalTime(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, clean)
	if err != nil {
		return time.Time{}, fmt.Errorf("parse time %q: %w", clean, err)
	}
	return t.UTC(), nil
}

func chatMessagesPayload(sphere string, messages []zulip.Message, stream, topic, query string, limit int) map[string]interface{} {
	items := make([]map[string]interface{}, 0, len(messages))
	for _, msg := range messages {
		items = append(items, map[string]interface{}{
			"id":                msg.ID,
			"sender_full_name":  msg.SenderName,
			"sender_email":      msg.SenderEmail,
			"stream":            msg.Stream,
			"topic":             msg.Topic,
			"timestamp":         msg.Timestamp.Format(time.RFC3339),
			"content":           msg.Content,
			"provider":          ProviderZulip,
			"provider_message":  fmt.Sprintf("zulip:%d", msg.ID),
			"conversation_name": conversationName(msg.Stream, msg.Topic),
		})
	}
	return map[string]interface{}{
		"sphere":   sphere,
		"provider": ProviderZulip,
		"stream":   strings.TrimSpace(stream),
		"topic":    strings.TrimSpace(topic),
		"query":    strings.TrimSpace(query),
		"messages": items,
		"count":    len(items),
		"limit":    limit,
	}
}

func conversationName(stream, topic string) string {
	stream = strings.TrimSpace(stream)
	topic = strings.TrimSpace(topic)
	if stream == "" {
		return topic
	}
	if topic == "" {
		return stream
	}
	return stream + " / " + topic
}

func stringArg(args map[string]interface{}, key string) string {
	if v, ok := args[key].(string); ok {
		return strings.TrimSpace(v)
	}
	return ""
}

func boundedLimit(args map[string]interface{}, def, max int) int {
	limit := def
	switch v := args["limit"].(type) {
	case float64:
		limit = int(v)
	case int:
		limit = v
	case int64:
		limit = int(v)
	}
	if limit <= 0 {
		return def
	}
	if limit > max {
		return max
	}
	return limit
}
