package mcp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	"github.com/sloppy-org/sloptools/internal/meetings"
)

// meetingSummaryDiagnosticNeedsRecipient is the structured diagnostic
// emitted for any draft whose recipient address could not be resolved
// from the brain people note, the per-user override, or the request
// arguments. Slopshell renders this as a compose-pane prompt.
const meetingSummaryDiagnosticNeedsRecipient = "needs_recipient"

// MeetingSummaryDraftResult is the structured payload returned by
// meeting.summary.draft and consumed by meeting.summary.send.
type MeetingSummaryDraftResult struct {
	Slug       string           `json:"slug"`
	Sphere     string           `json:"sphere"`
	Title      string           `json:"title,omitempty"`
	Date       string           `json:"date,omitempty"`
	Owner      string           `json:"owner,omitempty"`
	Attendees  []string         `json:"attendees,omitempty"`
	NotePath   string           `json:"note_path"`
	Share      meetingShareView `json:"share"`
	Drafts     []meetings.Draft `json:"drafts"`
	Recipients []string         `json:"recipients"`
	Count      int              `json:"count"`
}

type meetingShareView struct {
	Kind              string `json:"kind"`
	URL               string `json:"url,omitempty"`
	Live              bool   `json:"live"`
	AbsolutePath      string `json:"absolute_path"`
	VaultRelativePath string `json:"vault_relative_path,omitempty"`
	Permissions       string `json:"permissions"`
	HasState          bool   `json:"has_state"`
}

// dispatchMeetingTool registers the meeting.* MCP verbs. The verb
// names use dotted form to mirror the existing brain.* convention so
// agents can scope tool listings consistently.
func (s *Server) dispatchMeetingTool(name string, args map[string]interface{}) (map[string]interface{}, error) {
	switch name {
	case "meeting.summary.draft":
		return s.meetingSummaryDraft(args)
	case "meeting.summary.send":
		return s.meetingSummarySend(args)
	case "meeting.share.create":
		return s.meetingShareCreate(args)
	case "meeting.share.revoke":
		return s.meetingShareRevoke(args)
	default:
		return nil, errors.New("unknown meeting method: " + name)
	}
}

func (s *Server) meetingSummaryDraft(args map[string]interface{}) (map[string]interface{}, error) {
	ctx, err := s.loadMeetingSummaryContext(args)
	if err != nil {
		return nil, err
	}
	result, err := buildMeetingSummary(ctx, strings.TrimSpace(strArg(args, "recipient")))
	if err != nil {
		return nil, err
	}
	return meetingSummaryToMap(result), nil
}

func (s *Server) meetingSummarySend(args map[string]interface{}) (map[string]interface{}, error) {
	ctx, err := s.loadMeetingSummaryContext(args)
	if err != nil {
		return nil, err
	}
	recipient := strings.TrimSpace(strArg(args, "recipient"))
	if recipient == "" {
		return nil, errors.New("recipient is required for meeting.summary.send")
	}
	accountID, err := resolveMeetingMailAccountID(args, ctx.sphereCfg)
	if err != nil {
		return nil, err
	}
	override := strings.TrimSpace(strArg(args, "to"))
	result, err := buildMeetingSummary(ctx, recipient)
	if err != nil {
		return nil, err
	}
	if len(result.Drafts) == 0 {
		return nil, fmt.Errorf("no draft for recipient %q", recipient)
	}
	draft := result.Drafts[0]
	if override != "" {
		draft.Email = override
		draft.Diagnostic = ""
	}
	if draft.Email == "" {
		return nil, fmt.Errorf("recipient %q has no resolvable email; pass `to` or update brain people frontmatter", recipient)
	}
	requestCtx := context.Background()
	account, provider, err := s.ResolveMailAccount(requestCtx, accountID)
	if err != nil {
		return nil, err
	}
	defer provider.Close()
	send := MailSendRequest{AccountID: accountID, To: []string{draft.Email}, Subject: draft.Subject, Body: draft.Body, DraftOnly: !boolArg(args, "send_now")}
	composed, err := ExecuteMailSend(requestCtx, account, provider, send)
	if err != nil {
		return nil, err
	}
	out := mailComposeResultToMap(composed)
	out["recipient"] = draft.Recipient
	out["share_url"] = draft.ShareURL
	out["draft"] = draft
	out["slug"] = result.Slug
	return out, nil
}

func (s *Server) meetingShareCreate(args map[string]interface{}) (map[string]interface{}, error) {
	mctx, err := s.loadMeetingSummaryContext(args)
	if err != nil {
		return nil, err
	}
	state, _, err := meetings.LoadShareState(mctx.target)
	if err != nil {
		return nil, err
	}
	state.Permissions = meetings.ChooseSharePermissions(state.Permissions, strArg(args, "permissions"), mctx.sphereCfg.Share.Permissions)
	if days := intArg(args, "expiry_days", 0); days > 0 {
		state.ExpiryDays = days
	} else if state.ExpiryDays == 0 && mctx.sphereCfg.Share.ExpiryDays > 0 {
		state.ExpiryDays = mctx.sphereCfg.Share.ExpiryDays
	}
	if password, ok := args["password"]; ok {
		if value, ok := password.(bool); ok {
			state.Password = value
		}
	} else if !state.Password && mctx.sphereCfg.Share.Password {
		state.Password = true
	}
	suppliedURL := strings.TrimSpace(strArg(args, "url"))
	suppliedToken := strings.TrimSpace(strArg(args, "token"))
	if suppliedURL != "" {
		state.URL = suppliedURL
		if suppliedToken != "" {
			state.Token = suppliedToken
		}
	} else {
		record, err := meetings.CreateLiveShare(mctx.target, mctx.sphereCfg, state, mctx.target.Slug, s.newNextcloudShareClient)
		if err != nil {
			return nil, err
		}
		state.ID = record.ID
		state.URL = record.URL
		state.Token = record.Token
	}
	if state.CreatedAt == "" {
		state.CreatedAt = time.Now().UTC().Format(time.RFC3339)
	}
	if err := meetings.WriteShareState(mctx.target, state); err != nil {
		return nil, err
	}
	url, live := meetings.ShareLink(mctx.target, state, true, mctx.sphereCfg.Share)
	return map[string]interface{}{
		"slug":                mctx.target.Slug,
		"sphere":              mctx.sphere,
		"kind":                string(mctx.target.Kind),
		"absolute_path":       mctx.target.AbsolutePath,
		"vault_relative_path": mctx.target.VaultRelativePath,
		"state_path":          mctx.target.StatePath,
		"permissions":         state.Permissions,
		"expiry_days":         state.ExpiryDays,
		"password":            state.Password,
		"url":                 url,
		"live":                live,
		"share_id":            state.ID,
		"token":               state.Token,
	}, nil
}

func (s *Server) meetingShareRevoke(args map[string]interface{}) (map[string]interface{}, error) {
	mctx, err := s.loadMeetingSummaryContext(args)
	if err != nil {
		return nil, err
	}
	state, hadState, err := meetings.LoadShareState(mctx.target)
	if err != nil {
		return nil, err
	}
	revoked := false
	if hadState && strings.TrimSpace(state.ID) != "" {
		if err := meetings.RevokeLiveShare(mctx.sphereCfg, state.ID, s.newNextcloudShareClient); err != nil {
			return nil, err
		}
		revoked = true
	}
	if err := meetings.RemoveShareState(mctx.target); err != nil {
		return nil, err
	}
	return map[string]interface{}{
		"slug":            mctx.target.Slug,
		"sphere":          mctx.sphere,
		"state_path":      mctx.target.StatePath,
		"absolute_path":   mctx.target.AbsolutePath,
		"revoked":         true,
		"share_id":        state.ID,
		"share_id_purged": revoked,
	}, nil
}

type meetingSummaryContext struct {
	cfg       *brain.Config
	sphere    string
	sphereCfg meetings.SphereConfig
	vault     brain.Vault
	target    meetings.ShareTarget
}

func (s *Server) loadMeetingSummaryContext(args map[string]interface{}) (meetingSummaryContext, error) {
	cfg, err := brain.LoadConfig(s.brainConfigArg(args))
	if err != nil {
		return meetingSummaryContext{}, err
	}
	sphere := strings.TrimSpace(strArg(args, "sphere"))
	if sphere == "" {
		return meetingSummaryContext{}, errors.New("sphere is required")
	}
	vault, ok := cfg.Vault(brain.Sphere(sphere))
	if !ok {
		return meetingSummaryContext{}, fmt.Errorf("unknown vault %q", sphere)
	}
	slug := strings.TrimSpace(strArg(args, "slug"))
	if slug == "" {
		return meetingSummaryContext{}, errors.New("slug is required")
	}
	configPath, explicit, err := sloptoolsConfigPath(strArg(args, "sources_config"), "sources.toml")
	if err != nil {
		return meetingSummaryContext{}, err
	}
	meetingsCfg, err := meetings.Load(configPath, explicit)
	if err != nil {
		return meetingSummaryContext{}, err
	}
	sphereCfg, ok := meetingsCfg.Sphere(sphere)
	if !ok {
		sphereCfg = meetings.SphereConfig{Sphere: strings.ToLower(sphere), ShortMemoSeconds: meetings.DefaultShortMemoSeconds, Share: meetings.ShareConfig{Permissions: "edit"}}
	}
	if strings.TrimSpace(sphereCfg.MeetingsRoot) == "" {
		return meetingSummaryContext{}, fmt.Errorf("meetings_root is not configured for sphere %q", sphere)
	}
	target, err := meetings.ResolveShareTarget(sphereCfg.MeetingsRoot, slug)
	if err != nil {
		return meetingSummaryContext{}, err
	}
	target = target.AttachVaultRelative(vault.Root)
	return meetingSummaryContext{cfg: cfg, sphere: sphere, sphereCfg: sphereCfg, vault: vault, target: target}, nil
}

func buildMeetingSummary(ctx meetingSummaryContext, only string) (MeetingSummaryDraftResult, error) {
	notePath := meetingNotePath(ctx.target)
	src, err := os.ReadFile(notePath)
	if err != nil {
		return MeetingSummaryDraftResult{}, err
	}
	note := meetings.ParseSummary(ctx.target.Slug, string(src))
	if strings.TrimSpace(note.Owner) == "" {
		note.Owner = strings.TrimSpace(ctx.sphereCfg.Owner)
	}
	state, hasState, err := meetings.LoadShareState(ctx.target)
	if err != nil {
		return MeetingSummaryDraftResult{}, err
	}
	url, live := meetings.ShareLink(ctx.target, state, hasState, ctx.sphereCfg.Share)
	candidates, err := loadBrainPeopleCandidates(ctx.cfg, ctx.sphere)
	if err != nil {
		return MeetingSummaryDraftResult{}, err
	}
	recipients := note.SummaryRecipients()
	if only != "" {
		recipients = filterRecipientsTo(recipients, only)
	}
	resolver := newRecipientResolver(ctx.cfg, ctx.sphere, ctx.sphereCfg, candidates)
	drafts := make([]meetings.Draft, 0, len(recipients))
	for _, name := range recipients {
		canonical := meetings.ResolvePerson(ctx.sphereCfg.ResolveAlias(name), ctx.sphereCfg.OwnerAliases, candidates)
		emailAddr, diagnostic := resolver.resolve(canonical)
		draft := note.RenderDraft(canonical, emailAddr, meetings.DraftRequest{ShareURL: url})
		draft.Diagnostic = diagnostic
		drafts = append(drafts, draft)
	}
	meetings.SortDraftsByRecipient(drafts)
	out := MeetingSummaryDraftResult{
		Slug:       ctx.target.Slug,
		Sphere:     ctx.sphere,
		Title:      note.Title,
		Date:       note.Date,
		Owner:      note.Owner,
		Attendees:  note.Attendees,
		NotePath:   notePath,
		Drafts:     drafts,
		Recipients: recipients,
		Count:      len(drafts),
		Share: meetingShareView{
			Kind:              string(ctx.target.Kind),
			URL:               url,
			Live:              live,
			AbsolutePath:      ctx.target.AbsolutePath,
			VaultRelativePath: ctx.target.VaultRelativePath,
			Permissions:       ctx.sphereCfg.Share.Permissions,
			HasState:          hasState,
		},
	}
	return out, nil
}

func meetingNotePath(target meetings.ShareTarget) string {
	if target.Kind == meetings.ShareTargetFolder {
		return filepath.Join(target.AbsolutePath, "MEETING_NOTES.md")
	}
	return target.AbsolutePath
}

func filterRecipientsTo(names []string, only string) []string {
	want := strings.ToLower(strings.TrimSpace(only))
	if want == "" {
		return names
	}
	out := make([]string, 0, 1)
	for _, name := range names {
		if strings.EqualFold(strings.TrimSpace(name), only) || strings.ToLower(name) == want {
			out = append(out, name)
		}
	}
	if len(out) == 0 {
		out = append(out, only)
	}
	return out
}

type recipientResolver struct {
	cfg        *brain.Config
	sphere     string
	sphereCfg  meetings.SphereConfig
	candidates []string
}

func newRecipientResolver(cfg *brain.Config, sphere string, sphereCfg meetings.SphereConfig, candidates []string) recipientResolver {
	return recipientResolver{cfg: cfg, sphere: sphere, sphereCfg: sphereCfg, candidates: candidates}
}

func (r recipientResolver) resolve(name string) (string, string) {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return "", meetingSummaryDiagnosticNeedsRecipient
	}
	if email := r.lookupBrainEmail(clean); email != "" {
		return email, ""
	}
	if value := r.sphereCfg.PeopleEmail(clean); value != "" {
		return value, ""
	}
	return "", meetingSummaryDiagnosticNeedsRecipient
}

func (r recipientResolver) lookupBrainEmail(name string) string {
	rel := r.peopleNoteRel(name)
	if rel == "" {
		return ""
	}
	resolved, data, err := brain.ReadNoteFile(r.cfg, brain.Sphere(r.sphere), rel)
	if err != nil {
		return ""
	}
	_ = resolved
	note, _ := brain.ParseMarkdownNote(string(data), brain.MarkdownParseOptions{})
	if note == nil {
		return ""
	}
	for _, key := range []string{"email", "Email", "EMAIL"} {
		if value, ok := note.FrontMatterField(key); ok && value != nil {
			if clean := strings.TrimSpace(value.Value); clean != "" {
				return clean
			}
		}
	}
	return ""
}

func (r recipientResolver) peopleNoteRel(name string) string {
	clean := strings.TrimSpace(name)
	if clean == "" {
		return ""
	}
	for _, candidate := range r.candidates {
		if strings.EqualFold(candidate, clean) {
			return filepath.ToSlash(filepath.Join("brain", "people", candidate+".md"))
		}
	}
	return filepath.ToSlash(filepath.Join("brain", "people", clean+".md"))
}

func resolveMeetingMailAccountID(args map[string]interface{}, sphereCfg meetings.SphereConfig) (int64, error) {
	if v, ok, err := optionalInt64Arg(args, "account_id"); err != nil {
		return 0, err
	} else if ok && v != nil && *v > 0 {
		return *v, nil
	}
	if sphereCfg.MailAccountID > 0 {
		return sphereCfg.MailAccountID, nil
	}
	return 0, errors.New("account_id is required for meeting.summary.send (configure [meetings.<sphere>].mail_account_id or pass account_id)")
}

func meetingSummaryToMap(result MeetingSummaryDraftResult) map[string]interface{} {
	return map[string]interface{}{
		"slug":       result.Slug,
		"sphere":     result.Sphere,
		"title":      result.Title,
		"date":       result.Date,
		"owner":      result.Owner,
		"attendees":  result.Attendees,
		"note_path":  result.NotePath,
		"share":      result.Share,
		"drafts":     result.Drafts,
		"recipients": result.Recipients,
		"count":      result.Count,
	}
}
