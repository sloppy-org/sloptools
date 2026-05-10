package backend

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// AffinityForPick returns a deterministic UUID-shaped string for use as
// x-session-affinity. Each (runID, path, stage) triple maps to a unique
// slot so multi-round tool-call loops stay on the same slopgate peer,
// keeping the KV prefix cache warm.
func AffinityForPick(runID, path, stage string) string {
	h := sha256.Sum256([]byte(runID + "\x00" + path + "\x00" + stage))
	s := hex.EncodeToString(h[:])
	return fmt.Sprintf("%s-%s-%s-%s-%s", s[0:8], s[8:12], s[12:16], s[16:20], s[20:32])
}

// DefaultAffinityBase reads the x-session-affinity header value from
// ~/.config/opencode/opencode.json (the llamacpp or llamacpp-moe provider
// block). Used as fallback when brain.toml does not set affinity_uuid.
func DefaultAffinityBase() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return "sloptools-brain-default"
	}
	data, err := os.ReadFile(filepath.Join(home, ".config", "opencode", "opencode.json"))
	if err != nil {
		return "sloptools-brain-default"
	}
	var cfg map[string]interface{}
	if err := json.Unmarshal(data, &cfg); err != nil {
		return "sloptools-brain-default"
	}
	provider, _ := cfg["provider"].(map[string]interface{})
	for _, p := range provider {
		pm, _ := p.(map[string]interface{})
		opts, _ := pm["options"].(map[string]interface{})
		headers, _ := opts["headers"].(map[string]interface{})
		if v, ok := headers["x-session-affinity"].(string); ok && strings.TrimSpace(v) != "" {
			return v
		}
	}
	return "sloptools-brain-default"
}
