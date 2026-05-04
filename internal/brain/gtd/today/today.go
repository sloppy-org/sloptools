// Package gtdtoday implements the closed daily list that brain.gtd.today
// persists per day. The closed list is generated once per date, reaches
// at most a small fixed cap, and is durable so the Friday review can audit
// hit-rate. Commitments created after a day's list is generated do not
// appear in that same day's list.
package gtdtoday

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"gopkg.in/yaml.v3"
)

// HardItemCap is the upper bound on items in any single day's closed list.
// The number is taken from PLAN.md Step 23 / Forster's closed-list cure for
// open-ended commitment review.
const HardItemCap = 8

// CoreTrack labels the family floor (focus: core, in attention.md terms).
const CoreTrack = "core"

// Item is the durable view of a GTD commitment selected for one day's list.
// It carries enough information for the persisted Markdown to round-trip and
// for downstream review tools to operate without re-querying providers.
type Item struct {
	ID       string `json:"id" yaml:"id"`
	Title    string `json:"title" yaml:"title"`
	Source   string `json:"source,omitempty" yaml:"source,omitempty"`
	Path     string `json:"path,omitempty" yaml:"path,omitempty"`
	Queue    string `json:"queue,omitempty" yaml:"queue,omitempty"`
	Track    string `json:"track,omitempty" yaml:"track,omitempty"`
	Project  string `json:"project,omitempty" yaml:"project,omitempty"`
	Actor    string `json:"actor,omitempty" yaml:"actor,omitempty"`
	Due      string `json:"due,omitempty" yaml:"due,omitempty"`
	FollowUp string `json:"follow_up,omitempty" yaml:"follow_up,omitempty"`
	URL      string `json:"url,omitempty" yaml:"url,omitempty"`
	Pinned   bool   `json:"pinned,omitempty" yaml:"pinned,omitempty"`
}

// Snapshot is the durable record of one day's closed list.
type Snapshot struct {
	Sphere             string   `yaml:"sphere"`
	Date               string   `yaml:"date"`
	GeneratedAt        string   `yaml:"generated_at,omitempty"`
	IncludeFamilyFloor bool     `yaml:"include_family_floor,omitempty"`
	PinnedPaths        []string `yaml:"pinned_paths,omitempty"`
	Items              []Item   `yaml:"items"`
}

// Select picks at most cap items for the day's closed list. Pinned paths come
// first in the order supplied; if include_family_floor is true the next slots
// are filled with track=core items; remaining slots fall through to the
// already-sorted candidate pool. Closed/done items are dropped.
func Select(candidates []Item, pinnedPaths []string, includeFamilyFloor bool, cap int) []Item {
	if cap <= 0 || cap > HardItemCap {
		cap = HardItemCap
	}
	open := make([]Item, 0, len(candidates))
	for _, item := range candidates {
		if isClosedQueue(item.Queue) {
			continue
		}
		open = append(open, item)
	}

	out := make([]Item, 0, cap)
	seen := make(map[string]struct{}, cap)
	add := func(item Item, pinned bool) {
		if len(out) >= cap {
			return
		}
		if _, ok := seen[item.ID]; ok {
			return
		}
		seen[item.ID] = struct{}{}
		item.Pinned = pinned
		out = append(out, item)
	}

	for _, path := range pinnedPaths {
		clean := strings.TrimSpace(path)
		if clean == "" {
			continue
		}
		if found, ok := findByPath(open, clean); ok {
			add(found, true)
		}
	}
	if includeFamilyFloor {
		for _, item := range open {
			if strings.EqualFold(strings.TrimSpace(item.Track), CoreTrack) {
				add(item, false)
			}
		}
	}
	for _, item := range open {
		add(item, false)
	}
	return out
}

func findByPath(items []Item, path string) (Item, bool) {
	for _, item := range items {
		if item.Path == path {
			return item, true
		}
	}
	return Item{}, false
}

func isClosedQueue(queue string) bool {
	switch strings.ToLower(strings.TrimSpace(queue)) {
	case "done", "closed", "dropped":
		return true
	default:
		return false
	}
}

// Render writes the closed list as a Markdown note that round-trips through
// brain.ValidateMarkdownNote. Frontmatter carries the durable Snapshot.
func Render(snap Snapshot) (string, error) {
	if strings.TrimSpace(snap.Sphere) == "" {
		return "", errors.New("snapshot sphere is required")
	}
	if strings.TrimSpace(snap.Date) == "" {
		return "", errors.New("snapshot date is required")
	}
	frontmatter, err := marshalFrontmatter(snap)
	if err != nil {
		return "", err
	}
	body := buildBody(snap)
	return frontmatter + body, nil
}

func marshalFrontmatter(snap Snapshot) (string, error) {
	buf := strings.Builder{}
	buf.WriteString("---\n")
	buf.WriteString("kind: note\n")
	buf.WriteString("sphere: " + yamlQuote(snap.Sphere) + "\n")
	buf.WriteString("title: " + yamlQuote("GTD Today: "+snap.Date) + "\n")
	buf.WriteString("date: " + yamlQuote(snap.Date) + "\n")
	if strings.TrimSpace(snap.GeneratedAt) != "" {
		buf.WriteString("generated_at: " + yamlQuote(snap.GeneratedAt) + "\n")
	}
	buf.WriteString("frozen: true\n")
	if snap.IncludeFamilyFloor {
		buf.WriteString("include_family_floor: true\n")
	}
	if len(snap.PinnedPaths) > 0 {
		buf.WriteString("pinned_paths:\n")
		for _, path := range snap.PinnedPaths {
			buf.WriteString("  - " + yamlQuote(path) + "\n")
		}
	}
	itemsYAML, err := yaml.Marshal(map[string]interface{}{"items": snap.Items})
	if err != nil {
		return "", err
	}
	buf.WriteString(strings.TrimRight(string(itemsYAML), "\n") + "\n")
	buf.WriteString("---\n")
	return buf.String(), nil
}

func buildBody(snap Snapshot) string {
	buf := strings.Builder{}
	buf.WriteString("# GTD Today: " + snap.Date + "\n")
	pinned := make([]Item, 0, len(snap.Items))
	rest := make([]Item, 0, len(snap.Items))
	for _, item := range snap.Items {
		if item.Pinned {
			pinned = append(pinned, item)
		} else {
			rest = append(rest, item)
		}
	}
	if len(pinned) > 0 {
		buf.WriteString(fmt.Sprintf("\n## Pinned (%d)\n", len(pinned)))
		writeItemLines(&buf, pinned)
	}
	if len(rest) > 0 {
		buf.WriteString(fmt.Sprintf("\n## Today (%d)\n", len(rest)))
		writeItemLines(&buf, rest)
	}
	if len(pinned)+len(rest) == 0 {
		buf.WriteString("\n## Today (0)\n- No items selected.\n")
	}
	return buf.String()
}

func writeItemLines(buf *strings.Builder, items []Item) {
	for _, item := range items {
		buf.WriteString("- " + itemLine(item) + "\n")
	}
}

func itemLine(item Item) string {
	title := strings.TrimSpace(item.Title)
	if title == "" {
		title = strings.TrimSpace(item.ID)
	}
	if path := strings.TrimSpace(item.Path); path != "" {
		return "[" + title + "](" + path + ")"
	}
	if url := strings.TrimSpace(item.URL); url != "" {
		return "[" + title + "](" + url + ")"
	}
	return title
}

// Parse reads back a previously rendered closed list. Items missing from the
// frontmatter or with malformed shape produce an error: the file is the
// authoritative record and silent loss would defeat the audit goal.
func Parse(src string) (Snapshot, error) {
	note, diags := brain.ParseMarkdownNote(src, brain.MarkdownParseOptions{})
	if note == nil {
		return Snapshot{}, fmt.Errorf("parse closed list: %v", diags)
	}
	fm, ok := note.FrontMatter()
	if !ok || fm.Node == nil {
		return Snapshot{}, errors.New("closed list missing front matter")
	}
	var snap struct {
		Sphere             string   `yaml:"sphere"`
		Date               string   `yaml:"date"`
		GeneratedAt        string   `yaml:"generated_at"`
		Frozen             bool     `yaml:"frozen"`
		IncludeFamilyFloor bool     `yaml:"include_family_floor"`
		PinnedPaths        []string `yaml:"pinned_paths"`
		Items              []Item   `yaml:"items"`
	}
	if err := fm.Node.Decode(&snap); err != nil {
		return Snapshot{}, fmt.Errorf("decode closed list front matter: %w", err)
	}
	if !snap.Frozen {
		return Snapshot{}, errors.New("closed list front matter must declare frozen: true")
	}
	return Snapshot{
		Sphere:             snap.Sphere,
		Date:               snap.Date,
		GeneratedAt:        snap.GeneratedAt,
		IncludeFamilyFloor: snap.IncludeFamilyFloor,
		PinnedPaths:        snap.PinnedPaths,
		Items:              snap.Items,
	}, nil
}

// SortCandidates orders review items deterministically before selection.
// Pinning and family-floor inserts then build on this order.
func SortCandidates(items []Item) {
	sort.SliceStable(items, func(i, j int) bool {
		if r1, r2 := queueRank(items[i].Queue), queueRank(items[j].Queue); r1 != r2 {
			return r1 < r2
		}
		if items[i].Due != items[j].Due {
			return items[i].Due < items[j].Due
		}
		return strings.ToLower(items[i].Title) < strings.ToLower(items[j].Title)
	})
}

func queueRank(queue string) int {
	switch strings.ToLower(strings.TrimSpace(queue)) {
	case "next":
		return 0
	case "inbox":
		return 1
	case "waiting":
		return 2
	case "delegated":
		return 3
	case "review":
		return 4
	case "deferred":
		return 5
	case "someday":
		return 6
	case "done", "closed":
		return 7
	default:
		return 8
	}
}

func yamlQuote(value string) string {
	clean := strings.ReplaceAll(value, `"`, `\"`)
	return `"` + clean + `"`
}

// RunOptions captures everything Run needs to produce a closed list.
type RunOptions struct {
	Sphere             string
	Date               string
	PinnedPaths        []string
	IncludeFamilyFloor bool
	Limit              int
	Refresh            bool
}

// RunResult bundles the snapshot with status flags so the MCP layer can build
// a response without re-doing any I/O.
type RunResult struct {
	Snapshot Snapshot
	Updated  bool
	Frozen   bool
}

// Run loads or generates the closed list for one day. When the persisted file
// exists and Refresh is false, Run returns the parsed snapshot unchanged. When
// the file is missing or Refresh is true, Run pulls fresh candidates via the
// loader, sorts and selects, then renders + validates + writes the file.
func Run(filePath string, opts RunOptions, loadCandidates func() ([]Item, error), validate func(string) error) (RunResult, error) {
	if strings.TrimSpace(opts.Sphere) == "" {
		return RunResult{}, errors.New("sphere is required")
	}
	if strings.TrimSpace(opts.Date) == "" {
		return RunResult{}, errors.New("date is required")
	}
	if !opts.Refresh {
		if data, err := os.ReadFile(filePath); err == nil {
			snap, parseErr := Parse(string(data))
			if parseErr != nil {
				return RunResult{}, fmt.Errorf("read closed list %s: %w", filePath, parseErr)
			}
			return RunResult{Snapshot: snap, Updated: false, Frozen: true}, nil
		} else if !os.IsNotExist(err) {
			return RunResult{}, err
		}
	}
	candidates, err := loadCandidates()
	if err != nil {
		return RunResult{}, err
	}
	SortCandidates(candidates)
	selected := Select(candidates, opts.PinnedPaths, opts.IncludeFamilyFloor, opts.Limit)
	snap := Snapshot{
		Sphere:             opts.Sphere,
		Date:               opts.Date,
		GeneratedAt:        time.Now().UTC().Format(time.RFC3339),
		IncludeFamilyFloor: opts.IncludeFamilyFloor,
		PinnedPaths:        compact(opts.PinnedPaths),
		Items:              selected,
	}
	rendered, err := Render(snap)
	if err != nil {
		return RunResult{}, err
	}
	if validate != nil {
		if err := validate(rendered); err != nil {
			return RunResult{}, err
		}
	}
	if err := os.MkdirAll(filepath.Dir(filePath), 0o755); err != nil {
		return RunResult{}, err
	}
	if err := os.WriteFile(filePath, []byte(rendered), 0o644); err != nil {
		return RunResult{}, err
	}
	return RunResult{Snapshot: snap, Updated: true, Frozen: true}, nil
}

func compact(values []string) []string {
	out := make([]string, 0, len(values))
	seen := make(map[string]struct{}, len(values))
	for _, value := range values {
		clean := strings.TrimSpace(value)
		if clean == "" {
			continue
		}
		if _, ok := seen[clean]; ok {
			continue
		}
		seen[clean] = struct{}{}
		out = append(out, clean)
	}
	return out
}

// FormatDate normalizes any RFC3339 or YYYY-MM-DD input into YYYY-MM-DD.
// Empty inputs default to today's UTC date.
func FormatDate(raw string, now time.Time) (string, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return now.UTC().Format("2006-01-02"), nil
	}
	if len(clean) >= len("2006-01-02T15:04:05Z07:00") {
		if t, err := time.Parse(time.RFC3339, clean); err == nil {
			return t.UTC().Format("2006-01-02"), nil
		}
	}
	if t, err := time.Parse("2006-01-02", clean[:min(len(clean), len("2006-01-02"))]); err == nil {
		return t.Format("2006-01-02"), nil
	}
	return "", fmt.Errorf("invalid date %q (want YYYY-MM-DD or RFC3339)", raw)
}
