package mcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sloppy-org/sloptools/internal/brain"
	braingtd "github.com/sloppy-org/sloptools/internal/brain/gtd"
	"github.com/sloppy-org/sloptools/internal/email"
	"github.com/sloppy-org/sloptools/internal/providerdata"
	"github.com/sloppy-org/sloptools/internal/store"
)

func TestMailCommitmentListDerivesCommitmentsWithBoundedBodyFetches(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	day0 := time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)
	projectConfig, vaultConfig := writeMailCommitmentConfigs(t)
	askBody := "Could you review the draft by 2026-04-24?"
	newsletterBody := "Project newsletter and release notes."
	provider.listIDs = []string{"m1", "m2", "m3", "m4"}
	provider.pageIDs = []string{"m1", "m2", "m3", "m4"}
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {
			ID:       "m1",
			Subject:  "Draft",
			Sender:   "Ada Example <ada@example.com>",
			Date:     day0,
			Snippet:  "See below.",
			BodyText: &askBody,
			Labels:   []string{"Inbox"},
		},
		"m2": {
			ID:         "m2",
			Subject:    "Please send the recap by 2026-04-25",
			Sender:     "Work Account <me@example.com>",
			Recipients: []string{"bob@example.com"},
			Date:       day0.Add(1 * time.Hour),
			Snippet:    "Follow up on the recap.",
			Labels:     []string{"Sent"},
		},
		"m3": {
			ID:       "m3",
			Subject:  "Weekly newsletter",
			Sender:   "newsletter@example.com",
			Date:     day0.Add(2 * time.Hour),
			Snippet:  "Project news",
			BodyText: &newsletterBody,
			Labels:   []string{"Inbox"},
		},
		"m4": {
			ID:      "m4",
			Subject: "Auto-reply",
			Sender:  "auto-reply@example.com",
			Date:    day0.Add(3 * time.Hour),
			Snippet: "I am out of office.",
			Labels:  []string{"Inbox"},
		},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}

	got, err := s.callTool("mail_commitment_list", map[string]interface{}{"account_id": account.ID, "limit": 4, "body_limit": 1, "project_config": projectConfig, "vault_config": vaultConfig, "writeable": true})
	if err != nil {
		t.Fatalf("mail_commitment_list failed: %v", err)
	}
	if got["body_fetch_count"].(int) != 1 {
		t.Fatalf("body_fetch_count = %#v, want 1", got["body_fetch_count"])
	}
	if !equalStringSlices(provider.getMessagesFormats, []string{"metadata", "full"}) {
		t.Fatalf("getMessagesFormats = %#v", provider.getMessagesFormats)
	}

	records, ok := got["commitments"].([]mailCommitmentRecord)
	if !ok {
		t.Fatalf("commitments type = %T", got["commitments"])
	}
	if len(records) != 2 {
		t.Fatalf("commitments len = %d, want 2", len(records))
	}
	byID := map[string]mailCommitmentRecord{}
	for _, record := range records {
		byID[record.SourceID] = record
		if record.Commitment.Kind != "commitment" {
			t.Fatalf("record %s kind = %q", record.SourceID, record.Commitment.Kind)
		}
		if record.Artifact.Kind != store.ArtifactKindEmail {
			t.Fatalf("record %s artifact kind = %q", record.SourceID, record.Artifact.Kind)
		}
		if record.SourceURL == "" || !strings.HasPrefix(record.SourceURL, "sloptools://mail/") {
			t.Fatalf("record %s source_url = %q", record.SourceID, record.SourceURL)
		}
		if record.Artifact.RefURL == nil || !strings.HasPrefix(*record.Artifact.RefURL, "sloptools://mail/") {
			t.Fatalf("record %s artifact ref_url = %#v", record.SourceID, record.Artifact.RefURL)
		}
		if len(record.Commitment.SourceBindings) != 1 || !record.Commitment.SourceBindings[0].Writeable {
			t.Fatalf("record %s source binding = %#v", record.SourceID, record.Commitment.SourceBindings)
		}
	}

	m1 := byID["m1"]
	if m1.Commitment.Status != "next" {
		t.Fatalf("m1 status = %q, want next", m1.Commitment.Status)
	}
	if m1.Commitment.FollowUp != "2026-04-24" {
		t.Fatalf("m1 follow_up = %q, want 2026-04-24", m1.Commitment.FollowUp)
	}
	if m1.Commitment.Project != "[[projects/Paper]]" {
		t.Fatalf("m1 project = %q", m1.Commitment.Project)
	}
	if m1.Commitment.NextAction == "" || !strings.Contains(m1.Commitment.NextAction, "Ada Example") {
		t.Fatalf("m1 next_action = %q", m1.Commitment.NextAction)
	}
	if !containsAny(m1.Diagnostics, "needs_person_note: Ada Example") {
		t.Fatalf("m1 diagnostics = %#v", m1.Diagnostics)
	}

	m2 := byID["m2"]
	if m2.Commitment.Status != "waiting" {
		t.Fatalf("m2 status = %q, want waiting", m2.Commitment.Status)
	}
	if m2.Commitment.WaitingFor != "bob@example.com" {
		t.Fatalf("m2 waiting_for = %q", m2.Commitment.WaitingFor)
	}
	if m2.Commitment.FollowUp != "2026-04-25" {
		t.Fatalf("m2 follow_up = %q, want 2026-04-25", m2.Commitment.FollowUp)
	}

	if _, ok := byID["m3"]; ok {
		t.Fatal("newsletter produced a commitment")
	}
	if _, ok := byID["m4"]; ok {
		t.Fatal("auto-reply produced a commitment")
	}
}

func TestMailCommitmentCloseRequiresWriteableAndAppliesMailAction(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	provider.messages = map[string]*providerdata.EmailMessage{"m1": {ID: "m1", Subject: "Request", Date: time.Date(2026, time.April, 20, 9, 0, 0, 0, time.UTC)}}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) {
		return provider, nil
	}
	if _, err := s.callTool("mail_commitment_close", map[string]interface{}{"account_id": account.ID, "message_id": "m1"}); err == nil {
		t.Fatal("mail_commitment_close without writeable succeeded")
	}
	got, err := s.callTool("mail_commitment_close", map[string]interface{}{"account_id": account.ID, "message_id": "m1", "writeable": true})
	if err != nil {
		t.Fatalf("mail_commitment_close failed: %v", err)
	}
	if provider.lastAction != "archive" {
		t.Fatalf("lastAction = %q, want archive", provider.lastAction)
	}
	if len(provider.lastIDs) != 1 || provider.lastIDs[0] != "m1" {
		t.Fatalf("lastIDs = %#v", provider.lastIDs)
	}
	if got["closed"] != true {
		t.Fatalf("closed = %#v", got["closed"])
	}
}

func writeMailCommitmentConfigs(t *testing.T) (string, string) {
	t.Helper()
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "vault", "brain", "people"), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	projectConfig := filepath.Join(root, "projects.toml")
	if err := os.WriteFile(projectConfig, []byte(`
[[project]]
name = "Paper"
keywords = ["draft"]
people = ["Ada Example"]
`), 0o644); err != nil {
		t.Fatalf("WriteFile(projects): %v", err)
	}
	vaultConfig := filepath.Join(root, "vaults.toml")
	if err := os.WriteFile(vaultConfig, []byte(`
[[vault]]
sphere = "work"
root = "`+filepath.ToSlash(filepath.Join(root, "vault"))+`"
brain = "brain"
`), 0o644); err != nil {
		t.Fatalf("WriteFile(vaults): %v", err)
	}
	return projectConfig, vaultConfig
}

func equalStringSlices(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}

func containsAny(values []string, needle string) bool {
	for _, value := range values {
		if strings.Contains(value, needle) {
			return true
		}
	}
	return false
}

func TestMailFolderToLabelClassifiesPaths(t *testing.T) {
	cfg := writeMailLabelBrainConfig(t, []string{"RT-08", "Teaching"})
	cases := []struct {
		name        string
		folder      string
		wantProject string
		wantLabels  []string
	}{
		{name: "empty", folder: "", wantProject: "", wantLabels: nil},
		{name: "inbox-root", folder: "INBOX", wantProject: "", wantLabels: nil},
		{name: "inbox-root-empty-list", folder: "Inbox", wantProject: "", wantLabels: nil},
		{name: "single-subfolder-no-project", folder: "INBOX/General", wantProject: "", wantLabels: []string{"track/general"}},
		{name: "single-subfolder-project", folder: "INBOX/RT-08", wantProject: "[[projects/RT-08]]", wantLabels: nil},
		{name: "nested-no-project", folder: "INBOX/Teaching/WSD", wantProject: "", wantLabels: []string{"track/teaching", "track/wsd"}},
		{name: "nested-leaf-not-project", folder: "INBOX/Areas/Notes", wantProject: "", wantLabels: []string{"track/areas", "track/notes"}},
		{name: "nested-leaf-is-project", folder: "INBOX/Teaching/RT-08", wantProject: "[[projects/RT-08]]", wantLabels: []string{"track/teaching"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mailFolderToLabel(tc.folder, "work", cfg)
			if got.Project != tc.wantProject {
				t.Fatalf("Project = %q, want %q", got.Project, tc.wantProject)
			}
			if !equalStringSlices(got.Labels, tc.wantLabels) && !(len(got.Labels) == 0 && len(tc.wantLabels) == 0) {
				t.Fatalf("Labels = %#v, want %#v", got.Labels, tc.wantLabels)
			}
		})
	}
}

func TestMailMessageToGTDStatusCoversD5Table(t *testing.T) {
	future := time.Now().UTC().AddDate(0, 0, 7)
	past := time.Now().UTC().AddDate(0, 0, -7)
	cases := []struct {
		name     string
		message  *providerdata.EmailMessage
		waiting  string
		want     string
		followUp string
	}{
		{name: "unread-inbox", message: &providerdata.EmailMessage{Folder: "INBOX"}, want: "inbox"},
		{name: "unread-inbox-subfolder", message: &providerdata.EmailMessage{Folder: "INBOX/RT-08"}, want: "inbox"},
		{name: "read-inbox", message: &providerdata.EmailMessage{Folder: "INBOX", IsRead: true}, want: "next"},
		{name: "read-inbox-subfolder", message: &providerdata.EmailMessage{Folder: "INBOX/Teaching/WSD", IsRead: true}, want: "next"},
		{name: "read-flagged-future", message: &providerdata.EmailMessage{Folder: "INBOX", IsRead: true, IsFlagged: true, FollowUpAt: &future}, want: "deferred", followUp: future.Format("2006-01-02")},
		{name: "read-flagged-past-still-next", message: &providerdata.EmailMessage{Folder: "INBOX", IsRead: true, IsFlagged: true, FollowUpAt: &past}, want: "next"},
		{name: "read-flagged-no-due", message: &providerdata.EmailMessage{Folder: "INBOX", IsRead: true, IsFlagged: true}, want: "next"},
		{name: "waiting-folder", message: &providerdata.EmailMessage{Folder: "Waiting", IsRead: true}, want: "waiting"},
		{name: "waiting-folder-custom", message: &providerdata.EmailMessage{Folder: "OnHold", IsRead: true}, waiting: "OnHold", want: "waiting"},
		{name: "waiting-takes-precedence", message: &providerdata.EmailMessage{Folder: "Waiting", IsRead: false}, want: "waiting"},
		{name: "archive-non-inbox", message: &providerdata.EmailMessage{Folder: "Archive", IsRead: true}, want: "closed"},
		{name: "non-inbox-via-labels", message: &providerdata.EmailMessage{Labels: []string{"Archive"}, IsRead: true}, want: "closed"},
		{name: "labels-fall-back-inbox", message: &providerdata.EmailMessage{Labels: []string{"INBOX"}}, want: "inbox"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mailMessageToGTDStatus(tc.message, tc.waiting)
			if got.Status != tc.want {
				t.Fatalf("Status = %q, want %q (message=%#v)", got.Status, tc.want, tc.message)
			}
			if (tc.followUp != "" && got.FollowUp != tc.followUp) || (tc.want != "deferred" && got.FollowUp != "") {
				t.Fatalf("FollowUp = %q, want %q (status=%q)", got.FollowUp, tc.followUp, tc.want)
			}
		})
	}
}

func TestMailActionArchiveClosesBoundCommitment(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	cfgPath, vaultRoot := writeMailLabelTestVault(t, "work", []string{"RT-08"})
	s.brainConfigPath = cfgPath
	commitmentRel := "brain/commitments/mail/m1.md"
	writeMailLabelCommitment(t, vaultRoot, commitmentRel, "m1", "next")
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Topic", Folder: "Archive", Labels: []string{"Archive"}, IsRead: true, Date: time.Now().UTC()},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	got, err := s.callTool("mail_action", map[string]interface{}{"account_id": account.ID, "action": "archive", "message_ids": []interface{}{"m1"}})
	if err != nil {
		t.Fatalf("mail_action archive failed: %v", err)
	}
	refs := requireAffectedRefs(t, got)
	if !affectedHas(refs, "mail", "message", "m1", "") || !affectedHas(refs, "brain", "gtd_commitment", "", commitmentRel) {
		t.Fatalf("affected refs missing message+commitment: %#v", refs)
	}
	commitment := readCommitmentFile(t, filepath.Join(vaultRoot, filepath.FromSlash(commitmentRel)))
	if commitment.LocalOverlay.Status != "closed" {
		t.Fatalf("local_overlay.status = %q, want closed", commitment.LocalOverlay.Status)
	}
	if commitment.LocalOverlay.ClosedVia != "mail.mutation" {
		t.Fatalf("closed_via = %q, want mail.mutation", commitment.LocalOverlay.ClosedVia)
	}
}

func TestMailActionMoveBetweenInboxSubfoldersUpdatesLabels(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	cfgPath, vaultRoot := writeMailLabelTestVault(t, "work", []string{"RT-08"})
	s.brainConfigPath = cfgPath
	commitmentRel := "brain/commitments/mail/m1.md"
	writeMailLabelCommitmentWithLabels(t, vaultRoot, commitmentRel, "m1", "next", []string{"track/rt-08"}, "[[projects/RT-08]]")
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Topic", Folder: "INBOX", Labels: []string{"INBOX"}, IsRead: true, Date: time.Now().UTC()},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	if _, err := s.callTool("mail_action", map[string]interface{}{"account_id": account.ID, "action": "move_to_inbox", "message_ids": []interface{}{"m1"}}); err != nil {
		t.Fatalf("mail_action move_to_inbox failed: %v", err)
	}
	commitment := readCommitmentFile(t, filepath.Join(vaultRoot, filepath.FromSlash(commitmentRel)))
	if commitment.LocalOverlay.Status != "next" {
		t.Fatalf("status = %q, want next", commitment.LocalOverlay.Status)
	}
	if commitment.Project != "" {
		t.Fatalf("project = %q, want empty after move to INBOX", commitment.Project)
	}
	for _, label := range commitment.Labels {
		if strings.HasPrefix(strings.ToLower(label), "track/") {
			t.Fatalf("track label survived INBOX move: %q (labels=%v)", label, commitment.Labels)
		}
	}
}

func TestMailActionMoveIntoInboxSubfolderAddsTrackLabel(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	cfgPath, vaultRoot := writeMailLabelTestVault(t, "work", []string{"RT-08"})
	s.brainConfigPath = cfgPath
	commitmentRel := "brain/commitments/mail/m1.md"
	writeMailLabelCommitment(t, vaultRoot, commitmentRel, "m1", "next")
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Topic", Folder: "INBOX/RT-08", Labels: []string{"INBOX/RT-08"}, IsRead: true, Date: time.Now().UTC()},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	if _, err := s.callTool("mail_action", map[string]interface{}{"account_id": account.ID, "action": "move_to_folder", "message_ids": []interface{}{"m1"}, "folder": "INBOX/RT-08"}); err != nil {
		t.Fatalf("mail_action move_to_folder failed: %v", err)
	}
	commitment := readCommitmentFile(t, filepath.Join(vaultRoot, filepath.FromSlash(commitmentRel)))
	if commitment.LocalOverlay.Status != "next" {
		t.Fatalf("status = %q, want next", commitment.LocalOverlay.Status)
	}
	if commitment.Project != "[[projects/RT-08]]" {
		t.Fatalf("project = %q, want [[projects/RT-08]]", commitment.Project)
	}
	for _, label := range commitment.Labels {
		if strings.HasPrefix(strings.ToLower(label), "track/") {
			t.Fatalf("track label leaked despite project resolution: %v", commitment.Labels)
		}
	}
}

func TestMailActionMoveFromSubfolderToArchiveClosesCommitment(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	cfgPath, vaultRoot := writeMailLabelTestVault(t, "work", []string{"RT-08"})
	s.brainConfigPath = cfgPath
	commitmentRel := "brain/commitments/mail/m1.md"
	writeMailLabelCommitmentWithLabels(t, vaultRoot, commitmentRel, "m1", "next", []string{"track/rt-08"}, "[[projects/RT-08]]")
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Topic", Folder: "Archive", Labels: []string{"Archive"}, IsRead: true, Date: time.Now().UTC()},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	if _, err := s.callTool("mail_action", map[string]interface{}{"account_id": account.ID, "action": "move_to_folder", "message_ids": []interface{}{"m1"}, "folder": "Archive"}); err != nil {
		t.Fatalf("mail_action move_to_folder failed: %v", err)
	}
	commitment := readCommitmentFile(t, filepath.Join(vaultRoot, filepath.FromSlash(commitmentRel)))
	if commitment.LocalOverlay.Status != "closed" {
		t.Fatalf("status = %q, want closed", commitment.LocalOverlay.Status)
	}
}

func TestMailFlagSetWithFutureDateDefersBoundCommitment(t *testing.T) {
	s, account, provider := newMailToolsFixture(t)
	cfgPath, vaultRoot := writeMailLabelTestVault(t, "work", nil)
	s.brainConfigPath = cfgPath
	commitmentRel := "brain/commitments/mail/m1.md"
	writeMailLabelCommitment(t, vaultRoot, commitmentRel, "m1", "next")
	due := time.Now().UTC().AddDate(0, 0, 14)
	provider.messages = map[string]*providerdata.EmailMessage{
		"m1": {ID: "m1", Subject: "Defer me", Folder: "INBOX", Labels: []string{"INBOX"}, IsRead: true, IsFlagged: true, FollowUpAt: &due, Date: time.Now().UTC()},
	}
	s.newEmailProvider = func(context.Context, store.ExternalAccount) (email.EmailProvider, error) { return provider, nil }
	if _, err := s.callTool("mail_flag_set", map[string]interface{}{"account_id": account.ID, "message_ids": []interface{}{"m1"}, "status": "flagged", "due_at": due.Format(time.RFC3339)}); err != nil {
		t.Fatalf("mail_flag_set failed: %v", err)
	}
	commitment := readCommitmentFile(t, filepath.Join(vaultRoot, filepath.FromSlash(commitmentRel)))
	if commitment.LocalOverlay.Status != "deferred" {
		t.Fatalf("status = %q, want deferred", commitment.LocalOverlay.Status)
	}
	if commitment.LocalOverlay.FollowUp != due.Format("2006-01-02") {
		t.Fatalf("follow_up = %q, want %q", commitment.LocalOverlay.FollowUp, due.Format("2006-01-02"))
	}
}

func writeMailLabelBrainConfig(t *testing.T, projects []string) *brain.Config {
	t.Helper()
	root, _ := setupMailLabelVaultRoot(t, "work", projects)
	cfg, err := brain.NewConfig([]brain.Vault{
		{Sphere: "work", Root: filepath.Join(root, "work"), Brain: "brain"},
		{Sphere: "private", Root: filepath.Join(root, "private"), Brain: "brain"},
	})
	if err != nil {
		t.Fatalf("brain.NewConfig: %v", err)
	}
	return cfg
}

func writeMailLabelTestVault(t *testing.T, sphere string, projects []string) (string, string) {
	t.Helper()
	root, sphereRoot := setupMailLabelVaultRoot(t, sphere, projects)
	cfgPath := filepath.Join(root, "vaults.toml")
	body := "[[vault]]\nsphere = \"work\"\nroot = \"" + filepath.ToSlash(filepath.Join(root, "work")) + "\"\nbrain = \"brain\"\n\n" +
		"[[vault]]\nsphere = \"private\"\nroot = \"" + filepath.ToSlash(filepath.Join(root, "private")) + "\"\nbrain = \"brain\"\n"
	if err := os.WriteFile(cfgPath, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	return cfgPath, sphereRoot
}

func setupMailLabelVaultRoot(t *testing.T, sphere string, projects []string) (string, string) {
	t.Helper()
	root := t.TempDir()
	for _, s := range []string{"work", "private"} {
		if err := os.MkdirAll(filepath.Join(root, s, "brain", "projects"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if err := os.MkdirAll(filepath.Join(root, s, "brain", "commitments", "mail"), 0o755); err != nil {
			t.Fatalf("MkdirAll: %v", err)
		}
		if s != sphere {
			continue
		}
		for _, name := range projects {
			path := filepath.Join(root, s, "brain", "projects", name+".md")
			if err := os.WriteFile(path, []byte("---\nkind: project\n---\n"), 0o644); err != nil {
				t.Fatalf("WriteFile project: %v", err)
			}
		}
	}
	return root, filepath.Join(root, sphere)
}

func writeMailLabelCommitment(t *testing.T, vaultRoot, rel, messageID, status string) {
	writeMailLabelCommitmentWithLabels(t, vaultRoot, rel, messageID, status, nil, "")
}

func writeMailLabelCommitmentWithLabels(t *testing.T, vaultRoot, rel, messageID, status string, labels []string, project string) {
	t.Helper()
	path := filepath.Join(vaultRoot, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	header := fmt.Sprintf("---\nkind: commitment\nsphere: work\ntitle: Mail commitment\nstatus: %s\ncontext: mail\nnext_action: Reply\noutcome: Mail commitment\n", status)
	if project != "" {
		header += fmt.Sprintf("project: %q\n", project)
	}
	if len(labels) > 0 {
		header += "labels:\n"
		for _, label := range labels {
			header += "  - " + label + "\n"
		}
	}
	body := header + fmt.Sprintf("source_bindings:\n  - provider: mail\n    ref: %q\n---\n# Mail commitment\n\n## Summary\nMail.\n\n## Next Action\n- [ ] Reply\n\n## Evidence\n- mail:%s\n\n## Linked Items\n- None.\n\n## Review Notes\n- None.\n", messageID, messageID)
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
}

func affectedHas(refs []affectedRef, domain, kind, id, path string) bool {
	for _, ref := range refs {
		if ref.Domain == domain && ref.Kind == kind && (id == "" || ref.ID == id) && (path == "" || ref.Path == path) {
			return true
		}
	}
	return false
}

func readCommitmentFile(t *testing.T, path string) braingtd.Commitment {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	commitment, _, diags := braingtd.ParseCommitmentMarkdown(string(data))
	if len(diags) != 0 {
		t.Fatalf("parse diagnostics: %#v", diags)
	}
	return *commitment
}
