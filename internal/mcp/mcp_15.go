package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/mcp/gtdtoday"
	"github.com/sloppy-org/sloptools/internal/mcp/meetingkickoff"
)

func (s *Server) dispatchBrain(method string, args map[string]interface{}) (map[string]interface{}, error) {
	switch method {
	case "brain.config.get":
		return s.brainConfigGet(args)
	case "brain.vault.list":
		return s.brainVaultList(args)
	case "brain.note.parse":
		return s.brainNoteParse(args)
	case "brain.note.validate":
		return s.brainNoteValidate(args)
	case "brain.vault.validate":
		return s.brainVaultValidate(args)
	case "brain.links.resolve":
		return s.brainLinksResolve(args)
	case "brain.folder.parse":
		return s.brainNoteParse(args)
	case "brain.folder.validate":
		return s.brainNoteValidate(args)
	case "brain.folder.links":
		return s.brainFolderLinks(args)
	case "brain.folder.audit":
		return s.brainFolderAudit(args)
	case "brain.glossary.parse":
		return s.brainNoteParse(args)
	case "brain.glossary.validate":
		return s.brainNoteValidate(args)
	case "brain.attention.parse":
		return s.brainNoteParse(args)
	case "brain.attention.validate":
		return s.brainNoteValidate(args)
	case "brain.entities.candidates":
		return s.brainEntitiesCandidates(args)
	case "brain.gtd.parse":
		return s.brainGTDParseVault(args)
	case "brain.gtd.list":
		return s.brainGTDListVault(args)
	case "brain.gtd.tracks":
		return s.brainGTDTracks(args)
	case "brain.gtd.focus":
		return s.brainGTDFocus(args)
	case "brain.projects.render":
		return s.brainProjectsRender(args)
	case "brain.projects.list":
		return s.brainProjectsList(args)
	case "brain.gtd.write":
		return s.brainGTDWrite(args)
	case "brain.gtd.bulk_link":
		return s.brainGTDBulkLink(args)
	case "brain.gtd.bind":
		return s.brainGTDBind(args)
	case "brain.gtd.dedup_scan":
		return s.brainGTDDedupScan(args)
	case "brain.gtd.dedup_review_apply":
		return s.brainGTDDedupReviewApply(args)
	case "brain.gtd.dedup_history":
		return s.brainGTDDedupHistory(args)
	case "brain.gtd.review_list":
		return s.brainGTDReviewList(args)
	case "brain.gtd.set_status":
		return s.brainGTDSetStatus(args)
	case "brain.gtd.sync":
		return s.brainGTDSync(args)
	case "brain.gtd.organize":
		return s.brainGTDOrganize(args)
	case "brain.gtd.resurface":
		return s.brainGTDResurface(args)
	case "brain.gtd.dashboard":
		return s.brainGTDDashboard(args)
	case "brain.gtd.today":
		return s.brainGTDToday(args)
	case "brain.gtd.review_batch":
		return s.brainGTDReviewBatch(args)
	case "brain.gtd.ingest":
		return s.brainGTDIngest(args)
	case "brain.note.write":
		return s.brainNoteWrite(args)
	case "brain.people.dashboard":
		return s.brainPeopleDashboard(args)
	case "brain.people.render":
		return s.brainPeopleRender(args)
	case "brain.people.brief":
		return s.brainPeopleBrief(args)
	case "brain.meeting.kickoff":
		return s.runMeetingKickoff(args)
	case "brain.search", "brain_search":
		return s.brainSearch(args)
	case "brain.backlinks", "brain_backlinks":
		return s.brainBacklinks(args)
	default:
		return nil, errors.New("unknown brain method: " + method)
	}
}

func (s *Server) runMeetingKickoff(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sourcesPath, explicit, err := sloptoolsConfigPath(strArg(args, "sources_config"), "sources.toml")
	if err != nil {
		return nil, err
	}
	return meetingkickoff.Run(args, cfg, sourcesPath, explicit, s.newZulipMessagesProvider)
}

func (s *Server) brainSearch(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	query := strings.TrimSpace(strArg(args, "query"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if query == "" {
		return nil, errors.New("query is required")
	}
	mode, err := brain.ParseSearchMode(strArg(args, "mode"))
	if err != nil {
		return nil, err
	}
	results, err := brain.Search(context.Background(), cfg, brain.SearchOptions{
		Sphere: brain.Sphere(sphere),
		Query:  query,
		Mode:   mode,
		Limit:  intArg(args, "limit", 50),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": sphere, "mode": string(mode), "query": query, "results": results, "count": len(results)}, nil
}

func (s *Server) brainBacklinks(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	target := strings.TrimSpace(strArg(args, "target"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if target == "" {
		return nil, errors.New("target is required")
	}
	results, err := brain.Backlinks(context.Background(), cfg, brain.BacklinkOptions{
		Sphere: brain.Sphere(sphere),
		Target: target,
		Limit:  intArg(args, "limit", 50),
	})
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": sphere, "target": target, "results": results, "count": len(results)}, nil
}

func (s *Server) brainNoteWrite(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	path := strings.TrimSpace(strArg(args, "path"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	if path == "" {
		return nil, errors.New("path is required")
	}
	resolved, data, err := brain.ReadNoteFile(cfg, brain.Sphere(sphere), path)
	if err != nil {
		return nil, err
	}
	note, diags := brain.ParseMarkdownNote(string(data), brain.MarkdownParseOptions{})
	if note == nil {
		return nil, fmt.Errorf("failed to parse note %q", resolved.Rel)
	}
	updates := noteWriteUpdates(args)
	fields := make([]string, 0, len(updates))
	if fm := objectArg(updates, "frontmatter"); len(fm) > 0 {
		if err := applyNoteFrontMatter(note, fm, &fields); err != nil {
			return nil, err
		}
	}
	if sections := objectArg(updates, "sections"); len(sections) > 0 {
		if err := applyNoteSections(note, sections, &fields); err != nil {
			return nil, err
		}
	}
	for key, raw := range updates {
		switch key {
		case "frontmatter", "sections", "body", "markdown":
			continue
		}
		if err := note.SetFrontMatterField(key, raw); err != nil {
			return nil, err
		}
		fields = append(fields, key)
	}
	rendered, err := note.Render()
	if err != nil {
		return nil, err
	}
	if err := validateRenderedBrainNote(rendered); err != nil {
		return nil, err
	}
	if err := os.WriteFile(resolved.Path, []byte(rendered), 0o644); err != nil {
		return nil, err
	}
	return withAffected(
		map[string]interface{}{
			"source":      resolved,
			"fields":      fields,
			"diagnostics": diags,
			"count":       len(diags),
			"valid":       len(diags) == 0,
		},
		affectedRef{
			Domain:   "brain",
			Kind:     "note",
			Provider: "markdown",
			ID:       resolved.Rel,
			Path:     resolved.Rel,
			Sphere:   sphere,
		},
	), nil
}

func (s *Server) brainGTDResurface(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	path := strings.TrimSpace(strArg(args, "path"))
	changed := make([]string, 0)
	if path != "" {
		if resurfaceOneCommitment(cfg, brain.Sphere(sphere), path) {
			changed = append(changed, path)
		}
		return map[string]interface{}{"sphere": sphere, "count": len(changed), "paths": changed, "updated": len(changed) > 0}, nil
	}
	if err := brain.WalkVaultNotes(cfg, brain.Sphere(sphere), func(snapshot brain.NoteSnapshot) error {
		if snapshot.Kind != "commitment" {
			return nil
		}
		commitment, note, diags := braingtd.ParseCommitmentMarkdown(snapshot.Body)
		if len(diags) != 0 || !resurfaceCommitment(commitment, time.Now().UTC()) {
			return nil
		}
		if err := braingtd.ApplyCommitment(note, *commitment); err != nil {
			return err
		}
		rendered, err := note.Render()
		if err != nil {
			return err
		}
		if err := validateRenderedBrainGTD(rendered); err != nil {
			return err
		}
		if err := os.WriteFile(snapshot.Source.Path, []byte(rendered), 0o644); err != nil {
			return err
		}
		changed = append(changed, snapshot.Source.Rel)
		return nil
	}); err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": sphere, "count": len(changed), "paths": changed, "updated": len(changed) > 0}, nil
}

func resurfaceOneCommitment(cfg *brain.Config, sphere brain.Sphere, path string) bool {
	resolved, data, err := brain.ReadNoteFile(cfg, sphere, path)
	if err != nil {
		return false
	}
	commitment, note, diags := braingtd.ParseCommitmentMarkdown(string(data))
	if len(diags) != 0 || !resurfaceCommitment(commitment, time.Now().UTC()) {
		return false
	}
	if err := braingtd.ApplyCommitment(note, *commitment); err != nil {
		return false
	}
	rendered, err := note.Render()
	if err != nil {
		return false
	}
	if err := validateRenderedBrainGTD(rendered); err != nil {
		return false
	}
	return os.WriteFile(resolved.Path, []byte(rendered), 0o644) == nil
}

func noteWriteUpdates(args map[string]interface{}) map[string]interface{} {
	if updates := objectArg(args, "fields"); len(updates) > 0 {
		return updates
	}
	updates := make(map[string]interface{})
	for key, value := range args {
		switch key {
		case "config_path", "sphere", "path", "commitment":
			continue
		}
		updates[key] = value
	}
	return updates
}

func applyNoteFrontMatter(note *brain.MarkdownNote, updates map[string]interface{}, written *[]string) error {
	for key, value := range updates {
		if err := note.SetFrontMatterField(key, value); err != nil {
			return err
		}
		*written = append(*written, key)
	}
	return nil
}

func applyNoteSections(note *brain.MarkdownNote, updates map[string]interface{}, written *[]string) error {
	for name, raw := range updates {
		body, ok := raw.(string)
		if !ok {
			if fields := objectArg(map[string]interface{}{"section": raw}, "section"); len(fields) > 0 {
				if text, ok := stringArgFromMap(fields, "body"); ok {
					body = text
				}
			}
		}
		if body == "" {
			continue
		}
		if err := note.SetSectionBody(name, body); err != nil {
			return err
		}
		*written = append(*written, "section:"+name)
	}
	return nil
}

func resurfaceCommitment(commitment *braingtd.Commitment, now time.Time) bool {
	if commitment == nil {
		return false
	}
	if !strings.EqualFold(strings.TrimSpace(commitment.LocalOverlay.Status), "deferred") {
		return false
	}
	followUp := strings.TrimSpace(commitment.FollowUp)
	if followUp == "" {
		return false
	}
	dueDate := followUp
	if len(dueDate) >= len("2006-01-02T15:04:05Z07:00") {
		dueDate = dueDate[:10]
	}
	parsed, err := time.Parse("2006-01-02", dueDate)
	if err != nil {
		return false
	}
	if parsed.After(now.UTC()) {
		return false
	}
	commitment.LocalOverlay.Status = "next"
	return true
}

func slugify(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	var b strings.Builder
	lastDash := false
	for _, r := range value {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
			lastDash = false
		case unicode.IsSpace(r) || r == '-' || r == '_' || r == '.':
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		return "item"
	}
	return out
}

func validateRenderedBrainNote(rendered string) error {
	if diags := brain.ValidateMarkdownNote(rendered, brain.MarkdownParseOptions{}); len(diags) != 0 {
		return fmt.Errorf("rendered Markdown note failed validation: %s", formatBrainDiagnostics(diags))
	}
	return nil
}

func validateRenderedBrainGTD(rendered string) error {
	if diags := braingtd.ValidateRenderedCommitment(rendered); len(diags) != 0 {
		return fmt.Errorf("rendered GTD note failed validation: %s", formatBrainDiagnostics(diags))
	}
	return nil
}

func formatBrainDiagnostics(diags []brain.MarkdownDiagnostic) string {
	parts := make([]string, 0, len(diags))
	for _, diag := range diags {
		if diag.Line > 0 {
			parts = append(parts, fmt.Sprintf("line %d: %s", diag.Line, diag.Message))
			continue
		}
		parts = append(parts, diag.Message)
	}
	return strings.Join(parts, "; ")
}

func supportedIngestSource(source string) bool {
	switch strings.ToLower(strings.TrimSpace(source)) {
	case "meetings", "mail", "todoist", "github", "gitlab", "evernote":
		return true
	default:
		return false
	}
}

func displayIngestSource(source string) string {
	source = strings.TrimSpace(source)
	if source == "" {
		return "Source"
	}
	return strings.ToUpper(source[:1]) + strings.ToLower(source[1:])
}

func (s *Server) brainGTDToday(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	date, err := gtdtoday.FormatDate(strArg(args, "date"), time.Now())
	if err != nil {
		return nil, err
	}
	rel := filepath.ToSlash(filepath.Join("brain", "gtd", "today", date+".md"))
	resolved, err := brain.ResolveNotePath(cfg, brain.Sphere(sphere), rel)
	if err != nil {
		return nil, err
	}
	opts := gtdtoday.RunOptions{
		Sphere:             sphere,
		Date:               date,
		PinnedPaths:        stringListArg(args, "pinned_paths"),
		IncludeFamilyFloor: boolArg(args, "include_family_floor"),
		Limit:              intArg(args, "limit", gtdtoday.HardItemCap),
		Refresh:            boolArg(args, "refresh"),
	}
	result, err := gtdtoday.Run(resolved.Path, opts, func() ([]gtdtoday.Item, error) {
		return s.todayCandidates(args, sphere)
	}, validateRenderedBrainNote)
	if err != nil {
		return nil, err
	}
	return withAffected(map[string]interface{}{
		"sphere": sphere, "date": date, "path": resolved.Rel,
		"items": result.Snapshot.Items, "count": len(result.Snapshot.Items),
		"pinned_paths":         result.Snapshot.PinnedPaths,
		"include_family_floor": result.Snapshot.IncludeFamilyFloor,
		"frozen":               result.Frozen, "updated": result.Updated,
	}, brainCommitmentAffectedRef(sphere, resolved.Rel)), nil
}
