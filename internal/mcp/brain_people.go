package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/brain/people"
	"github.com/sloppy-org/sloptools/internal/brain/peoplebrief"
)

const currentOpenLoopsHeading = "Current open loops"

type personOpenLoop struct {
	Path         string `json:"path"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	WaitingFor   string `json:"waiting_for,omitempty"`
	Due          string `json:"due,omitempty"`
	FollowUp     string `json:"follow_up,omitempty"`
	ClosedAt     string `json:"closed_at,omitempty"`
	LastEvidence string `json:"last_evidence_at,omitempty"`
}

type resolvedPersonNote struct {
	Name string
	Path string
	Rel  string
}

func (s *Server) brainPeopleDashboard(args map[string]interface{}) (map[string]interface{}, error) {
	dashboard, person, err := s.buildPersonDashboard(args)
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere":          strArg(args, "sphere"),
		"person":          person.Name,
		"person_path":     person.Rel,
		"waiting_on_them": dashboard["waiting_on_them"],
		"i_owe_them":      dashboard["i_owe_them"],
		"recently_closed": dashboard["recently_closed"],
	}, nil
}

func (s *Server) brainPeopleBrief(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	person, err := resolvePersonNote(vault, strArg(args, "name"))
	if err != nil {
		return nil, err
	}
	src, err := os.ReadFile(person.Path)
	if err != nil {
		return nil, err
	}
	note, _ := brain.ParseMarkdownNote(string(src), brain.MarkdownParseOptions{})
	notes, err := readDedupNotes(vault)
	if err != nil {
		return nil, err
	}
	commitments := make([]peoplebrief.Commitment, 0, len(notes))
	for _, n := range notes {
		commitments = append(commitments, peoplebrief.CommitmentFromCommitment(n.Entry.Path, n.Entry.Commitment, commitmentClosed(n.Entry.Commitment)))
	}
	meeting, err := peoplebrief.LatestMeetingNote(vault.Root, vault.BrainRoot(), person.Name)
	if err != nil {
		return nil, err
	}
	out := map[string]interface{}{
		"sphere":         sphere,
		"person":         person.Name,
		"person_path":    person.Rel,
		"frontmatter":    peoplebrief.Frontmatter(note),
		"status_bullets": peoplebrief.StatusBullets(note, strArg(args, "status_section"), intArg(args, "status_limit", peoplebrief.DefaultStatusLimit)),
		"open_loops":     peoplebrief.ClassifyOpenLoops(commitments, person.Name),
	}
	if meeting != nil {
		out["latest_meeting"] = meeting
	}
	personEmail := strArg(args, "email")
	if personEmail == "" {
		personEmail = peoplebrief.PersonEmail(note, string(src))
	}
	if personEmail == "" {
		out["diagnostics"] = []string{"no email address available for person"}
		return out, nil
	}
	account, provider, err := s.mailProviderForTool(args)
	if err != nil {
		out["diagnostics"] = []string{"mail lookup unavailable: " + err.Error()}
		return out, nil
	}
	defer provider.Close()
	mail, err := peoplebrief.LatestPersonMail(context.Background(), provider, account.ID, personEmail)
	if err != nil {
		out["diagnostics"] = []string{"mail lookup failed: " + err.Error()}
		return out, nil
	}
	if mail != nil {
		out["latest_mail"] = mail
	}
	return out, nil
}

func (s *Server) brainPeopleRender(args map[string]interface{}) (map[string]interface{}, error) {
	dashboard, person, err := s.buildPersonDashboard(args)
	if err != nil {
		if isNeedsPersonNote(err) {
			return map[string]interface{}{"sphere": strArg(args, "sphere"), "changed": false, "diagnostics": []string{err.Error()}}, nil
		}
		return nil, err
	}
	data, err := os.ReadFile(person.Path)
	if err != nil {
		return nil, err
	}
	rendered := renderOpenLoopsSection(string(data), person.Rel, dashboard)
	if rendered == string(data) {
		return map[string]interface{}{"sphere": strArg(args, "sphere"), "person": person.Name, "person_path": person.Rel, "changed": false}, nil
	}
	if err := validateRenderedBrainNote(rendered); err != nil {
		return nil, err
	}
	if err := os.WriteFile(person.Path, []byte(rendered), 0o644); err != nil {
		return nil, err
	}
	return map[string]interface{}{"sphere": strArg(args, "sphere"), "person": person.Name, "person_path": person.Rel, "changed": true}, nil
}

func (s *Server) brainPeopleMonthlyIndex(args map[string]interface{}) (map[string]interface{}, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, fmt.Errorf("unknown vault %q", sphere)
	}
	res, err := people.WriteMonthlyIndexes(vault.BrainRoot(), boolArg(args, "dry_run"))
	if err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"sphere":  vault.Sphere,
		"vault":   vault,
		"months":  res.Months,
		"writes":  res.Writes,
		"dry_run": res.DryRun,
	}, nil
}

func (s *Server) buildPersonDashboard(args map[string]interface{}) (map[string][]personOpenLoop, resolvedPersonNote, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return nil, resolvedPersonNote{}, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return nil, resolvedPersonNote{}, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return nil, resolvedPersonNote{}, fmt.Errorf("unknown vault %q", sphere)
	}
	person, err := resolvePersonNote(vault, strArg(args, "name"))
	if err != nil {
		return nil, resolvedPersonNote{}, err
	}
	notes, err := readDedupNotes(vault)
	if err != nil {
		return nil, resolvedPersonNote{}, err
	}
	return aggregatePersonLoops(notes, person.Name, intArg(args, "recent_limit", 10), time.Now().UTC()), person, nil
}

func aggregatePersonLoops(notes []dedupNote, person string, recentLimit int, now time.Time) map[string][]personOpenLoop {
	out := map[string][]personOpenLoop{"waiting_on_them": {}, "i_owe_them": {}, "recently_closed": {}}
	for _, note := range notes {
		commitment := note.Entry.Commitment
		status := effectiveCommitmentStatus(commitment)
		waitingForPerson := peoplebrief.PersonFieldMatches(commitment.WaitingFor, person)
		peopleIncludePerson := peoplebrief.PeopleFieldMatches(commitment.People, person)
		touchesPerson := waitingForPerson || peopleIncludePerson
		switch {
		case (status == "waiting" || status == "deferred") && waitingForPerson:
			out["waiting_on_them"] = append(out["waiting_on_them"], openLoopFromNote(note))
		case (status == "next" || status == "inbox") && peopleIncludePerson && !waitingForPerson:
			out["i_owe_them"] = append(out["i_owe_them"], openLoopFromNote(note))
		case commitmentClosed(commitment) && touchesPerson:
			if recentClosed(commitment, now) {
				out["recently_closed"] = append(out["recently_closed"], openLoopFromNote(note))
			}
		}
	}
	sortLoops(out["waiting_on_them"])
	sortLoops(out["i_owe_them"])
	sortRecentLoops(out["recently_closed"])
	if recentLimit <= 0 {
		recentLimit = 10
	}
	if len(out["recently_closed"]) > recentLimit {
		out["recently_closed"] = out["recently_closed"][:recentLimit]
	}
	return out
}

func openLoopFromNote(note dedupNote) personOpenLoop {
	commitment := note.Entry.Commitment
	return personOpenLoop{
		Path:         note.Entry.Path,
		Title:        commitmentTitle(commitment),
		Status:       effectiveCommitmentStatus(commitment),
		WaitingFor:   commitment.WaitingFor,
		Due:          commitment.Due,
		FollowUp:     commitment.FollowUp,
		ClosedAt:     closedAt(commitment),
		LastEvidence: commitment.LastEvidenceAt,
	}
}

func renderOpenLoopsSection(src, personRel string, dashboard map[string][]personOpenLoop) string {
	body := formatOpenLoopsBody(personRel, dashboard)
	section := "## " + currentOpenLoopsHeading + "\n" + body
	start, end, ok := h2SectionBounds(src, currentOpenLoopsHeading)
	if !ok {
		return strings.TrimRight(src, "\n") + "\n\n" + section
	}
	return src[:start] + section + src[end:]
}

func formatOpenLoopsBody(personRel string, dashboard map[string][]personOpenLoop) string {
	waiting := dashboard["waiting_on_them"]
	owed := dashboard["i_owe_them"]
	closed := dashboard["recently_closed"]
	if len(waiting) == 0 && len(owed) == 0 && len(closed) == 0 {
		return "\n_None at present._\n"
	}
	var b strings.Builder
	writeLoopGroup(&b, "Waiting on them", personRel, waiting)
	writeLoopGroup(&b, "I owe them", personRel, owed)
	writeLoopGroup(&b, "Recently closed", personRel, closed)
	return b.String()
}

func writeLoopGroup(b *strings.Builder, title, personRel string, loops []personOpenLoop) {
	if len(loops) == 0 {
		return
	}
	b.WriteByte('\n')
	b.WriteString("### " + title + "\n")
	for _, item := range loops {
		b.WriteString("- [" + item.Title + "](" + relativeMarkdownPath(personRel, item.Path) + ")")
		if item.Due != "" {
			b.WriteString(" due " + item.Due)
		} else if item.FollowUp != "" {
			b.WriteString(" follow up " + item.FollowUp)
		} else if item.ClosedAt != "" {
			b.WriteString(" closed " + item.ClosedAt)
		}
		b.WriteByte('\n')
	}
}

func h2SectionBounds(src, heading string) (int, int, bool) {
	lines := strings.SplitAfter(src, "\n")
	offset := 0
	start := -1
	for _, line := range lines {
		if isH2(line, heading) {
			start = offset
			break
		}
		offset += len(line)
	}
	if start < 0 {
		return 0, 0, false
	}
	end := len(src)
	offset = start + len(linesAt(src[start:])[0])
	for _, line := range linesAt(src[offset:]) {
		if isAnyH2(line) {
			end = offset
			break
		}
		offset += len(line)
	}
	return start, end, true
}

func resolvePersonNote(vault brain.Vault, rawName string) (resolvedPersonNote, error) {
	query := strings.TrimSpace(rawName)
	if query == "" {
		return resolvedPersonNote{}, errors.New("name is required")
	}
	entries, err := os.ReadDir(filepath.Join(vault.BrainRoot(), "people"))
	if err != nil {
		if os.IsNotExist(err) {
			return resolvedPersonNote{}, fmt.Errorf("needs_person_note: %s", query)
		}
		return resolvedPersonNote{}, err
	}
	matches := matchingPersonNotes(entries, query)
	if len(matches) == 0 {
		return resolvedPersonNote{}, fmt.Errorf("needs_person_note: %s", query)
	}
	if len(matches) > 1 {
		return resolvedPersonNote{}, fmt.Errorf("ambiguous_person_note: %s", query)
	}
	name := matches[0]
	path := filepath.Join(vault.BrainRoot(), "people", name+".md")
	rel, err := filepath.Rel(vault.Root, path)
	if err != nil {
		return resolvedPersonNote{}, err
	}
	return resolvedPersonNote{Name: name, Path: path, Rel: filepath.ToSlash(rel)}, nil
}

func isNeedsPersonNote(err error) bool {
	return err != nil && strings.HasPrefix(err.Error(), "needs_person_note: ")
}

func matchingPersonNotes(entries []os.DirEntry, query string) []string {
	normalizedQuery := peoplebrief.NormalizePersonName(query)
	var exact []string
	var token []string
	for _, entry := range entries {
		if entry.IsDir() || filepath.Ext(entry.Name()) != ".md" {
			continue
		}
		name := strings.TrimSuffix(entry.Name(), ".md")
		normalizedName := peoplebrief.NormalizePersonName(name)
		if normalizedName == normalizedQuery {
			exact = append(exact, name)
			continue
		}
		if peoplebrief.SingleToken(normalizedQuery) && peoplebrief.NameContainsToken(normalizedName, normalizedQuery) {
			token = append(token, name)
		}
	}
	if len(exact) > 0 {
		sort.Strings(exact)
		return exact
	}
	sort.Strings(token)
	return token
}

func effectiveCommitmentStatus(commitment braingtd.Commitment) string {
	status := strings.ToLower(strings.TrimSpace(commitment.LocalOverlay.Status))
	if status == "" {
		status = strings.ToLower(strings.TrimSpace(commitment.Status))
	}
	return status
}

func commitmentTitle(commitment braingtd.Commitment) string {
	title := strings.TrimSpace(commitment.Title)
	if title != "" {
		return title
	}
	return strings.TrimSpace(commitment.Outcome)
}

func recentClosed(commitment braingtd.Commitment, now time.Time) bool {
	closed := closedAt(commitment)
	if closed == "" {
		closed = commitment.LastEvidenceAt
	}
	t, err := parseLoopTime(closed)
	if err != nil {
		return false
	}
	return !t.Before(now.AddDate(0, 0, -14)) && !t.After(now.Add(24*time.Hour))
}

func closedAt(commitment braingtd.Commitment) string {
	return strings.TrimSpace(commitment.LocalOverlay.ClosedAt)
}

func parseLoopTime(raw string) (time.Time, error) {
	clean := strings.TrimSpace(raw)
	if clean == "" {
		return time.Time{}, errors.New("empty time")
	}
	if t, err := time.Parse(time.RFC3339, clean); err == nil {
		return t.UTC(), nil
	}
	return time.Parse("2006-01-02", clean)
}

func sortLoops(items []personOpenLoop) {
	sort.Slice(items, func(i, j int) bool {
		if items[i].Path == items[j].Path {
			return items[i].Title < items[j].Title
		}
		return items[i].Path < items[j].Path
	})
}

func sortRecentLoops(items []personOpenLoop) {
	sort.Slice(items, func(i, j int) bool {
		return loopSortTime(items[i]).After(loopSortTime(items[j]))
	})
}

func loopSortTime(item personOpenLoop) time.Time {
	if t, err := parseLoopTime(item.ClosedAt); err == nil {
		return t
	}
	if t, err := parseLoopTime(item.LastEvidence); err == nil {
		return t
	}
	return time.Time{}
}

func relativeMarkdownPath(fromRel, toRel string) string {
	fromDir := filepath.Dir(filepath.FromSlash(fromRel))
	rel, err := filepath.Rel(fromDir, filepath.FromSlash(toRel))
	if err != nil {
		return filepath.ToSlash(toRel)
	}
	return filepath.ToSlash(rel)
}

func isH2(line, heading string) bool {
	return strings.TrimSpace(line) == "## "+heading
}

func isAnyH2(line string) bool {
	trimmed := strings.TrimSpace(line)
	return strings.HasPrefix(trimmed, "## ") && !strings.HasPrefix(trimmed, "### ")
}

func linesAt(src string) []string {
	if src == "" {
		return []string{""}
	}
	return strings.SplitAfter(src, "\n")
}
