package mcp

import (
	"context"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/mcp/gtdfocus"
	"github.com/sloppy-org/sloptools/internal/mcp/gtdtoday"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/sourceitems"
	"github.com/sloppy-org/sloptools/internal/store"
	"github.com/sloppy-org/sloptools/pkg/taskgtd"
)

func (s *Server) brainGTDTracks(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	tracksCfg, err := s.loadGTDTracksConfig(args)
	if err != nil {
		return nil, err
	}
	return gtdfocus.Tracks(cfg, strings.TrimSpace(strArg(args, "sphere")), tracksCfg)
}

func (s *Server) brainGTDFocus(args map[string]interface{}) (map[string]interface{}, error) {
	st, err := s.requireStore()
	if err != nil {
		return nil, err
	}
	return gtdfocus.Focus(st, strings.TrimSpace(strArg(args, "sphere")), args)
}

type gtdReviewItem struct {
	ID           string   `json:"id"`
	Source       string   `json:"source"`
	SourceRef    string   `json:"source_ref,omitempty"`
	Title        string   `json:"title"`
	Status       string   `json:"status"`
	Queue        string   `json:"queue"`
	Kind         string   `json:"kind,omitempty"`
	URL          string   `json:"url,omitempty"`
	Path         string   `json:"path,omitempty"`
	Due          string   `json:"due,omitempty"`
	FollowUp     string   `json:"follow_up,omitempty"`
	Labels       []string `json:"labels,omitempty"`
	Actor        string   `json:"actor,omitempty"`
	DelegatedTo  string   `json:"delegated_to,omitempty"`
	Project      string   `json:"project,omitempty"`
	Track        string   `json:"track,omitempty"`
	ParentID     string   `json:"parent_id,omitempty"`
	ExistingPath string   `json:"existing_path,omitempty"`
}

type gtdReviewBuild struct {
	items          []gtdReviewItem
	bindings       map[string]string
	seen           map[string]struct{}
	errors         []string
	duplicateCount int
}

func (s *Server) brainGTDReviewList(args map[string]interface{}) (map[string]interface{}, error) {
	build := gtdReviewBuild{bindings: make(map[string]string), seen: make(map[string]struct{})}
	sources, explicit := gtdReviewSources(args)
	if sources["markdown"] {
		if err := s.addMarkdownGTDItems(args, &build); err != nil {
			return nil, err
		}
	}
	if sources["mail"] {
		s.addMailGTDItems(args, &build)
	}
	if sources["github"] || sources["gitlab"] || sources["source"] || sources["sources"] || sources["issues"] {
		s.addIssueGTDItems(args, &build)
	}
	if sources["tasks"] || sources["todoist"] || sources["google_tasks"] {
		if explicit {
			build.errors = append(build.errors, "tasks source is deprecated as a default; pass explicit sources=['tasks'] to opt in")
			s.addTaskGTDItems(args, &build)
		}
	}
	if sources["evernote"] && explicit {
		build.errors = append(build.errors, "evernote source is deprecated; knowledge import only, no GTD items returned")
	}
	build.items = filterGTDReviewItems(build.items, args)
	sort.SliceStable(build.items, func(i, j int) bool {
		if build.items[i].Queue != build.items[j].Queue {
			return taskgtd.QueueRank(build.items[i].Queue) < taskgtd.QueueRank(build.items[j].Queue)
		}
		if build.items[i].Due != build.items[j].Due {
			return build.items[i].Due < build.items[j].Due
		}
		return strings.ToLower(build.items[i].Title) < strings.ToLower(build.items[j].Title)
	})
	limit := intArg(args, "limit", 0)
	if limit > 0 && len(build.items) > limit {
		build.items = build.items[:limit]
	}
	tracksCfg, err := s.loadGTDTracksConfig(args)
	if err != nil {
		return nil, err
	}
	overWIP := overWIPTracks(build.items, tracksCfg, strArg(args, "sphere"))
	return map[string]interface{}{
		"sphere":             strArg(args, "sphere"),
		"items":              build.items,
		"count":              len(build.items),
		"queue_counts":       queueCounts(build.items),
		"duplicate_skipped":  build.duplicateCount,
		"errors":             build.errors,
		"over_wip":           overWIP,
		"markdown_canonical": true,
	}, nil
}

// gtdReviewSources returns the source set and whether the caller passed
// `sources` explicitly. Default is markdown + live live mail + GitHub +
// GitLab; tasks/todoist/google_tasks/evernote remain accepted but emit a
// deprecation warning when explicitly requested.
func gtdReviewSources(args map[string]interface{}) (map[string]bool, bool) {
	values := stringListArg(args, "sources")
	explicit := len(values) > 0
	if !explicit {
		values = []string{"markdown", "mail", "github", "gitlab"}
	}
	out := make(map[string]bool, len(values))
	for _, value := range values {
		out[strings.ToLower(strings.TrimSpace(value))] = true
	}
	return out, explicit
}

func (s *Server) addMarkdownGTDItems(args map[string]interface{}, build *gtdReviewBuild) error {
	notes, _, err := s.loadDedupNotes(args)
	if err != nil {
		return err
	}
	for _, note := range notes {
		if isMailShadowMarkdownPath(note.Entry.Path) {
			continue
		}
		item := gtdReviewItemFromCommitment(note)
		for _, binding := range note.Entry.Commitment.SourceBindings {
			id := binding.StableID()
			if id != "" {
				build.bindings[id] = note.Entry.Path
			}
		}
		build.add(item)
	}
	return nil
}

// isMailShadowMarkdownPath flags brain/commitments/mail/** files. The
// live mail branch surfaces those items directly off provider state, so
// keeping markdown shadows in the walker would double-count.
func isMailShadowMarkdownPath(rel string) bool {
	clean := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	return strings.HasPrefix(clean, "brain/commitments/mail/")
}

// addMailGTDItems surfaces commitments derived live from mail provider
// state across every enabled mail-capable account in the requested
// sphere. The D5 mapping is applied in mail_commitment_list, so this
// branch just unwraps the records into review items.
func (s *Server) addMailGTDItems(args map[string]interface{}, build *gtdReviewBuild) {
	st, err := s.requireStore()
	if err != nil {
		build.errors = append(build.errors, err.Error())
		return
	}
	accounts, err := st.ListExternalAccounts(strings.TrimSpace(strArg(args, "sphere")))
	if err != nil {
		build.errors = append(build.errors, err.Error())
		return
	}
	for _, account := range accounts {
		if !account.Enabled || !emailCapableProvider(account.Provider) {
			continue
		}
		s.collectMailReviewItems(args, account, build)
	}
}

func (s *Server) collectMailReviewItems(args map[string]interface{}, account store.ExternalAccount, build *gtdReviewBuild) {
	mailArgs := map[string]interface{}{"account_id": account.ID, "limit": intArg(args, "limit", 50)}
	for _, key := range []string{"folder", "project_config", "vault_config"} {
		if value, ok := args[key]; ok {
			mailArgs[key] = value
		}
	}
	result, err := s.mailCommitmentList(mailArgs)
	if err != nil {
		build.errors = append(build.errors, fmt.Sprintf("%s %q: %v", account.Provider, account.AccountName, err))
		return
	}
	records, _ := result["commitments"].([]mailCommitmentRecord)
	for _, record := range records {
		build.addOrSkipExisting(gtdReviewItemFromMailRecord(account, record))
	}
}

func gtdReviewItemFromMailRecord(account store.ExternalAccount, record mailCommitmentRecord) gtdReviewItem {
	commitment := record.Commitment
	status := strings.ToLower(strings.TrimSpace(commitment.Status))
	sourceRef := fmt.Sprintf("mail:%s:%d:%s", strings.ToLower(strings.TrimSpace(account.Sphere)), account.ID, strings.TrimSpace(record.SourceID))
	return gtdReviewItem{
		ID:          sourceRef,
		Source:      "mail",
		SourceRef:   sourceRef,
		Title:       firstNonEmpty(commitment.Title, record.Message.Subject, record.SourceID),
		Status:      status,
		Queue:       taskgtd.Queue(status, commitment.FollowUp, time.Now().UTC()),
		URL:         record.SourceURL,
		FollowUp:    commitment.FollowUp,
		Labels:      append([]string(nil), commitment.Labels...),
		Actor:       firstNonEmpty(commitment.DelegatedTo, commitment.WaitingFor, commitment.Actor),
		DelegatedTo: commitment.DelegatedTo,
		Project:     commitment.Project,
		Track:       commitment.EffectiveTrack(),
	}
}

func (s *Server) addIssueGTDItems(args map[string]interface{}, build *gtdReviewBuild) {
	for _, dir := range stringListArg(args, "project_dirs") {
		provider, err := sourceProviderForReview(dir, strArg(args, "provider"))
		if err != nil {
			build.errors = append(build.errors, err.Error())
			continue
		}
		items, err := provider.List(context.Background())
		if err != nil {
			build.errors = append(build.errors, err.Error())
			continue
		}
		for _, item := range items {
			build.addOrSkipExisting(gtdReviewItemFromSourceItem(item))
		}
	}
}

func sourceProviderForReview(projectDir, providerName string) (sourceitems.Provider, error) {
	if strings.TrimSpace(projectDir) == "" {
		projectDir = "."
	}
	switch strings.ToLower(strings.TrimSpace(providerName)) {
	case "", "auto":
		detected, err := sourceitems.DetectProvider(projectDir)
		if err != nil {
			return nil, err
		}
		return sourceProviderForReview(projectDir, detected)
	case sourceitems.GitHubProviderName:
		return sourceitems.NewGitHubProvider(projectDir)
	case sourceitems.GitLabProviderName:
		return sourceitems.NewGitLabProvider(projectDir)
	default:
		return nil, fmt.Errorf("unsupported source provider %q", providerName)
	}
}

func (b *gtdReviewBuild) addOrSkipExisting(item gtdReviewItem) {
	shadowPath := b.bindings[item.ID]
	if shadowPath != "" && isIssueShadowMarkdownPath(shadowPath) {
		if item.Status != "closed" {
			b.removeMarkdownItem("markdown:" + shadowPath)
			b.add(item)
			return
		}
		b.duplicateCount++
		return
	}
	if shadowPath != "" {
		b.duplicateCount++
		return
	}
	b.add(item)
}

// removeMarkdownItem drops a previously-added markdown shadow item so a
// live API result can win the dedup. Used by the issue branch when an
// open issue arrives with a markdown shadow at commitments/{github,gitlab}/.
func (b *gtdReviewBuild) removeMarkdownItem(id string) {
	for i, existing := range b.items {
		if existing.ID == id {
			b.items = append(b.items[:i], b.items[i+1:]...)
			delete(b.seen, id)
			b.duplicateCount++
			return
		}
	}
}

// isIssueShadowMarkdownPath flags brain/commitments/{github,gitlab}/**
// markdown files. Live API state for matching open issues replaces the
// markdown shadow until the API state reaches closed.
func isIssueShadowMarkdownPath(rel string) bool {
	clean := strings.TrimSpace(strings.ReplaceAll(rel, "\\", "/"))
	return strings.HasPrefix(clean, "brain/commitments/github/") || strings.HasPrefix(clean, "brain/commitments/gitlab/")
}

func (b *gtdReviewBuild) add(item gtdReviewItem) {
	if strings.TrimSpace(item.ID) == "" {
		item.ID = item.Source + ":" + item.Title
	}
	if _, ok := b.seen[item.ID]; ok {
		b.duplicateCount++
		return
	}
	b.seen[item.ID] = struct{}{}
	b.items = append(b.items, item)
}

func gtdReviewItemFromCommitment(note dedupNote) gtdReviewItem {
	c := note.Entry.Commitment
	status := effectiveGTDStatus(c)
	return gtdReviewItem{
		ID: "markdown:" + note.Entry.Path, Source: "markdown",
		Title:  firstNonEmpty(c.Outcome, c.Title, c.NextAction, filepath.Base(note.Entry.Path)),
		Status: status, Queue: taskgtd.Queue(status, c.FollowUp, time.Now().UTC()),
		Path:        note.Entry.Path,
		Due:         c.Due,
		Labels:      append([]string(nil), c.Labels...),
		Actor:       firstNonEmpty(c.DelegatedTo, c.WaitingFor, c.Actor),
		DelegatedTo: c.DelegatedTo,
		Project:     c.Project,
		Track:       c.EffectiveTrack(), FollowUp: c.FollowUp,
	}
}

func filterGTDReviewItems(items []gtdReviewItem, args map[string]interface{}) []gtdReviewItem {
	if len(items) == 0 {
		return items
	}
	out := items[:0]
	for _, item := range items {
		if gtdReviewItemMatches(item, args) {
			out = append(out, item)
		}
	}
	return out
}

func gtdReviewItemMatches(item gtdReviewItem, args map[string]interface{}) bool {
	if queue := strings.TrimSpace(strArg(args, "queue")); queue != "" && !strings.EqualFold(item.Queue, queue) {
		return false
	}
	if project := strings.TrimSpace(strArg(args, "project")); project != "" && !strings.EqualFold(item.Project, project) {
		return false
	}
	if track := strings.TrimSpace(strArg(args, "track")); track != "" && !strings.EqualFold(item.Track, track) {
		return false
	}
	return reviewTimeMatches(item.Due, args, "due_after", false) &&
		reviewTimeMatches(item.Due, args, "due_before", true) &&
		reviewTimeMatches(item.FollowUp, args, "follow_up_after", false) &&
		reviewTimeMatches(item.FollowUp, args, "follow_up_before", true)
}

func reviewTimeMatches(value string, args map[string]interface{}, key string, before bool) bool {
	boundText := strings.TrimSpace(strArg(args, key))
	if boundText == "" {
		return true
	}
	bound := parseRFC3339OrDate(boundText)
	if bound.IsZero() {
		return false
	}
	t := parseRFC3339OrDate(value)
	if t.IsZero() {
		return false
	}
	if before {
		return t.Before(bound) || t.Equal(bound)
	}
	return t.After(bound) || t.Equal(bound)
}

func gtdReviewItemFromSourceItem(source providerdata.SourceItem) gtdReviewItem {
	binding := braingtd.SourceBinding{Provider: source.Provider, Ref: strings.TrimPrefix(source.SourceRef, source.Provider+":"), URL: source.URL}
	status := sourceItemStatus(source)
	followUp := sourceItemFollowUp(source)
	return gtdReviewItem{
		ID:        binding.StableID(),
		Source:    source.Provider,
		SourceRef: binding.Ref,
		Title:     source.Title,
		Status:    status,
		Queue:     taskgtd.Queue(status, followUp, time.Now().UTC()),
		Kind:      source.Kind,
		URL:       source.URL,
		FollowUp:  followUp,
		Labels:    append([]string(nil), source.Labels...),
		Actor:     firstNonEmpty(strings.Join(source.Assignees, ", "), source.Author),
		Project:   source.Container,
		Track:     braingtd.TrackFromLabels(source.Labels),
	}
}

// sourceItemStatus maps a GitHub/GitLab issue or pull-request to its GTD
// status per the locked plan: closed → closed; review-requested or
// reviewer assignment → waiting; future follow-up label or due → deferred;
// open with assignee → next.
func sourceItemStatus(source providerdata.SourceItem) string {
	if sourceClosed(source.State) {
		return "closed"
	}
	if hasFollowUpLabel(source.Labels) {
		return "deferred"
	}
	if len(source.Reviewers) > 0 {
		return "waiting"
	}
	switch strings.ToLower(strings.TrimSpace(source.ReviewStatus)) {
	case "review_requested", "changes_requested":
		return "waiting"
	}
	return "next"
}

func sourceItemFollowUp(source providerdata.SourceItem) string {
	for _, label := range source.Labels {
		if strings.HasPrefix(strings.ToLower(strings.TrimSpace(label)), "follow-up:") {
			parts := strings.SplitN(label, ":", 2)
			if len(parts) == 2 {
				return strings.TrimSpace(parts[1])
			}
		}
	}
	return ""
}

func hasFollowUpLabel(labels []string) bool {
	for _, label := range labels {
		lower := strings.ToLower(strings.TrimSpace(label))
		if lower == "follow-up" || strings.HasPrefix(lower, "follow-up:") || lower == "deferred" {
			return true
		}
	}
	return false
}

func effectiveGTDStatus(c braingtd.Commitment) string {
	if strings.TrimSpace(c.LocalOverlay.Status) != "" {
		return strings.ToLower(strings.TrimSpace(c.LocalOverlay.Status))
	}
	return strings.ToLower(strings.TrimSpace(c.Status))
}

func sourceClosed(state string) bool {
	switch strings.ToLower(strings.TrimSpace(state)) {
	case "closed", "merged", "done":
		return true
	default:
		return false
	}
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if clean := strings.TrimSpace(value); clean != "" {
			return clean
		}
	}
	return ""
}

// todayCandidates pulls the underlying review_list output and projects each
// item into the gtdtoday Item shape. Lives next to the review_list helpers so
// brain.gtd.today does not need to know how the review pipeline is wired.
func (s *Server) todayCandidates(args map[string]interface{}, sphere string) ([]gtdtoday.Item, error) {
	reviewArgs := copyArgs(args)
	reviewArgs["sphere"] = sphere
	delete(reviewArgs, "limit")
	reviewResult, err := s.brainGTDReviewList(reviewArgs)
	if err != nil {
		return nil, err
	}
	reviewItems, _ := reviewResult["items"].([]gtdReviewItem)
	out := make([]gtdtoday.Item, 0, len(reviewItems))
	for _, item := range reviewItems {
		out = append(out, gtdtoday.Item{
			ID: item.ID, Title: item.Title, Source: item.Source, Path: item.Path,
			Queue: item.Queue, Track: item.Track, Project: item.Project,
			Actor: item.Actor, Due: item.Due, FollowUp: item.FollowUp, URL: item.URL,
		})
	}
	return out, nil
}
