package gtdfocus

import (
	"errors"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/braincatalog"
	"github.com/sloppy-org/sloptools/pkg/taskgtd"
)

const (
	trackKeyPrefix   = "brain.gtd.focus.track"
	projectKeyPrefix = "brain.gtd.focus.project"
	actionKeyPrefix  = "brain.gtd.focus.action"
	updatedKeyPrefix = "brain.gtd.focus.updated"
)

type Store interface {
	AppState(string) (string, error)
	SetAppState(string, string) error
}

type SourceRef struct {
	Source string `json:"source,omitempty"`
	Ref    string `json:"ref,omitempty"`
	Path   string `json:"path,omitempty"`
}

type State struct {
	Sphere    string    `json:"sphere"`
	Track     string    `json:"track,omitempty"`
	Project   SourceRef `json:"project,omitempty"`
	Action    SourceRef `json:"action,omitempty"`
	UpdatedAt string    `json:"updated_at,omitempty"`
}

func Tracks(cfg *brain.Config, sphere string, tracksCfg *TracksConfig) (map[string]interface{}, error) {
	if strings.TrimSpace(sphere) == "" {
		return nil, errors.New("sphere is required")
	}
	items, err := braincatalog.ListGTDVault(cfg, brain.Sphere(sphere), braincatalog.GTDListFilter{})
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	counts := map[string]int{}
	wip := map[string]int{}
	for _, item := range items {
		track := strings.TrimSpace(item.Track)
		if track == "" {
			continue
		}
		counts[track]++
		if WIPCounts(item.Status, item.FollowUp, now) {
			wip[track]++
		}
	}
	for _, configured := range tracksCfg.SphereTracks(sphere) {
		if _, ok := counts[configured.Name]; !ok {
			counts[configured.Name] = 0
		}
	}
	tracks := make([]map[string]interface{}, 0, len(counts))
	for track, count := range counts {
		entry := map[string]interface{}{
			"id":             track,
			"label":          track,
			"label_value":    "track/" + track,
			"open_count":     count,
			"open_wip_count": wip[track],
		}
		if cfgTrack, ok := tracksCfg.Lookup(sphere, track); ok && cfgTrack.WIPLimit > 0 {
			entry["wip_limit"] = cfgTrack.WIPLimit
			entry["wip_status"] = WIPStatus(wip[track], cfgTrack.WIPLimit)
		}
		tracks = append(tracks, entry)
	}
	sort.Slice(tracks, func(i, j int) bool {
		return strings.ToLower(tracks[i]["id"].(string)) < strings.ToLower(tracks[j]["id"].(string))
	})
	return map[string]interface{}{"sphere": sphere, "tracks": tracks, "count": len(tracks), "canonical": "labels"}, nil
}

// WIPCounts reports whether a GTD item with the given status and follow-up
// counts toward work-in-progress. Items count when their effective queue is
// either "next" or "in_progress"; waiting/deferred (still in the
// future)/someday/done/review do not. Deferred items whose follow-up has
// elapsed roll into next via taskgtd.Queue and therefore count.
func WIPCounts(status, followUp string, now time.Time) bool {
	queue := taskgtd.Queue(strings.ToLower(strings.TrimSpace(status)), followUp, now)
	return queue == taskgtd.StatusNext || queue == taskgtd.StatusInProgress
}

// DashboardWIPRows builds the dashboard's WIP rows for the configured
// tracks of sphere. Tracks without a positive wip_limit are skipped.
// Items count toward a track when their effective queue is "next" or
// "in_progress" (see WIPCounts).
func DashboardWIPRows(items []braincatalog.GTDListItem, sphere string, tracksCfg *TracksConfig, now time.Time) []braincatalog.DashboardWIPRow {
	configured := tracksCfg.SphereTracks(sphere)
	if len(configured) == 0 {
		return nil
	}
	counts := map[string]int{}
	for _, item := range items {
		if !WIPCounts(item.Status, item.FollowUp, now) {
			continue
		}
		track := strings.ToLower(strings.TrimSpace(item.Track))
		if track == "" {
			continue
		}
		counts[track]++
	}
	rows := make([]braincatalog.DashboardWIPRow, 0, len(configured))
	for _, track := range configured {
		if track.WIPLimit <= 0 {
			continue
		}
		count := counts[track.Name]
		rows = append(rows, braincatalog.DashboardWIPRow{
			Track:  track.Name,
			Limit:  track.WIPLimit,
			Count:  count,
			Status: WIPStatus(count, track.WIPLimit),
		})
	}
	return rows
}

func Focus(st Store, sphere string, args map[string]interface{}) (map[string]interface{}, error) {
	if strings.TrimSpace(sphere) == "" {
		return nil, errors.New("sphere is required")
	}
	state, err := readState(st, sphere)
	if err != nil {
		return nil, err
	}
	applyArgs(&state, args)
	if mutating(args) {
		state.UpdatedAt = time.Now().UTC().Format(time.RFC3339)
		if err := writeState(st, state); err != nil {
			return nil, err
		}
	}
	return map[string]interface{}{"focus": state, "canonical": "sloptools"}, nil
}

func applyArgs(state *State, args map[string]interface{}) {
	if track := strArg(args, "track"); track != "" {
		state.Track = track
		state.Project = SourceRef{}
		state.Action = SourceRef{}
	}
	if boolArg(args, "clear_track") {
		state.Track = ""
		state.Project = SourceRef{}
		state.Action = SourceRef{}
	}
	if ref := sourceRefFromArgs(args, "project"); !ref.Empty() {
		state.Project = ref
		state.Action = SourceRef{}
	}
	if boolArg(args, "clear_project") {
		state.Project = SourceRef{}
		state.Action = SourceRef{}
	}
	if ref := sourceRefFromArgs(args, "action"); !ref.Empty() {
		state.Action = ref
	}
	if boolArg(args, "clear_action") {
		state.Action = SourceRef{}
	}
}

func mutating(args map[string]interface{}) bool {
	keys := []string{"track", "clear_track", "project_source", "project_ref", "project_path", "clear_project", "action_source", "action_ref", "action_path", "clear_action"}
	for _, key := range keys {
		if _, ok := args[key]; ok {
			return true
		}
	}
	return false
}

func sourceRefFromArgs(args map[string]interface{}, prefix string) SourceRef {
	source := strArg(args, prefix+"_source")
	ref := strArg(args, prefix+"_ref")
	path := strArg(args, prefix+"_path")
	if path != "" && source == "" && ref == "" {
		source = "markdown"
		ref = path
	}
	return SourceRef{Source: source, Ref: ref, Path: path}
}

func readState(st Store, sphere string) (State, error) {
	state := State{Sphere: sphere}
	var err error
	if state.Track, err = st.AppState(key(trackKeyPrefix, sphere)); err != nil {
		return state, err
	}
	if state.Project, err = readRef(st, projectKeyPrefix, sphere, state.Track); err != nil {
		return state, err
	}
	if state.Action, err = readRef(st, actionKeyPrefix, sphere, state.Track); err != nil {
		return state, err
	}
	state.UpdatedAt, _ = st.AppState(key(updatedKeyPrefix, sphere))
	return state, nil
}

func writeState(st Store, state State) error {
	if err := st.SetAppState(key(trackKeyPrefix, state.Sphere), state.Track); err != nil {
		return err
	}
	if err := writeRef(st, projectKeyPrefix, state.Sphere, state.Track, state.Project); err != nil {
		return err
	}
	if err := writeRef(st, actionKeyPrefix, state.Sphere, state.Track, state.Action); err != nil {
		return err
	}
	return st.SetAppState(key(updatedKeyPrefix, state.Sphere), state.UpdatedAt)
}

func readRef(st Store, prefix, sphere, track string) (SourceRef, error) {
	base := key(prefix, sphere, track)
	source, err := st.AppState(base + ".source")
	if err != nil {
		return SourceRef{}, err
	}
	ref, err := st.AppState(base + ".ref")
	if err != nil {
		return SourceRef{}, err
	}
	path, err := st.AppState(base + ".path")
	if err != nil {
		return SourceRef{}, err
	}
	return SourceRef{Source: source, Ref: ref, Path: path}, nil
}

func writeRef(st Store, prefix, sphere, track string, ref SourceRef) error {
	base := key(prefix, sphere, track)
	values := map[string]string{".source": ref.Source, ".ref": ref.Ref, ".path": ref.Path}
	for suffix, value := range values {
		if err := st.SetAppState(base+suffix, value); err != nil {
			return err
		}
	}
	return nil
}

func (r SourceRef) Empty() bool {
	return strings.TrimSpace(r.Source) == "" && strings.TrimSpace(r.Ref) == "" && strings.TrimSpace(r.Path) == ""
}

func strArg(args map[string]interface{}, key string) string {
	v, _ := args[key].(string)
	return strings.TrimSpace(v)
}

func boolArg(args map[string]interface{}, key string) bool {
	v, _ := args[key].(bool)
	return v
}

func key(parts ...string) string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		out = append(out, url.QueryEscape(strings.TrimSpace(part)))
	}
	return strings.Join(out, ".")
}
