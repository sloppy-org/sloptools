package store_test

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	. "github.com/sloppy-org/sloptools/internal/store"
	_ "modernc.org/sqlite"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

var _ *Store

func TestLinkArtifactToWorkspaceRejectsHomeWorkspace(t *testing.T) {
	s := newTestStore(t)
	workspaceDir := filepath.Join(t.TempDir(), "alpha")
	if err := os.MkdirAll(workspaceDir, 0o755); err != nil {
		t.Fatalf("mkdir alpha: %v", err)
	}
	workspace, err := s.CreateWorkspace("Alpha", workspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace() error: %v", err)
	}
	refPath := filepath.Join(workspaceDir, "spec.md")
	if err := os.WriteFile(refPath, []byte("# spec\n"), 0o644); err != nil {
		t.Fatalf("write spec.md: %v", err)
	}
	title := "spec.md"
	artifact, err := s.CreateArtifact(ArtifactKindMarkdown, &refPath, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	if err := s.LinkArtifactToWorkspace(workspace.ID, artifact.ID); err == nil || err.Error() != "artifact already belongs to workspace" {
		t.Fatalf("LinkArtifactToWorkspace(home) error = %v, want home-workspace rejection", err)
	}
}

func TestCreateItemInfersWorkspaceFromArtifactWithoutOverridingExplicitWorkspace(t *testing.T) {
	s := newTestStore(t)
	artifactWorkspaceDir := filepath.Join(t.TempDir(), "artifact-workspace")
	explicitWorkspaceDir := filepath.Join(t.TempDir(), "explicit-workspace")
	artifactWorkspace, err := s.CreateWorkspace("Artifact Workspace", artifactWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(artifact) error: %v", err)
	}
	explicitWorkspace, err := s.CreateWorkspace("Explicit Workspace", explicitWorkspaceDir)
	if err != nil {
		t.Fatalf("CreateWorkspace(explicit) error: %v", err)
	}
	docPath := filepath.Join(artifactWorkspaceDir, "docs", "task.md")
	artifact, err := s.CreateArtifact(ArtifactKindDocument, &docPath, nil, nil, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	inferredItem, err := s.CreateItem("Infer from artifact", ItemOptions{ArtifactID: &artifact.ID})
	if err != nil {
		t.Fatalf("CreateItem(inferred) error: %v", err)
	}
	if inferredItem.WorkspaceID == nil || *inferredItem.WorkspaceID != artifactWorkspace.ID {
		t.Fatalf("CreateItem(inferred).WorkspaceID = %v, want %d", inferredItem.WorkspaceID, artifactWorkspace.ID)
	}
	explicitItem, err := s.CreateItem("Keep explicit workspace", ItemOptions{ArtifactID: &artifact.ID, WorkspaceID: &explicitWorkspace.ID})
	if err != nil {
		t.Fatalf("CreateItem(explicit) error: %v", err)
	}
	if explicitItem.WorkspaceID == nil || *explicitItem.WorkspaceID != explicitWorkspace.ID {
		t.Fatalf("CreateItem(explicit).WorkspaceID = %v, want %d", explicitItem.WorkspaceID, explicitWorkspace.ID)
	}
}

func initGitRepoWithRemote(t *testing.T, dirPath, remoteURL string) {
	t.Helper()
	if err := exec.Command("git", "init", dirPath).Run(); err != nil {
		t.Fatalf("git init %s: %v", dirPath, err)
	}
	if err := exec.Command("git", "-C", dirPath, "remote", "add", "origin", remoteURL).Run(); err != nil {
		t.Fatalf("git remote add origin %s: %v", dirPath, err)
	}
}

func TestExternalAccountStoreCRUD(t *testing.T) {
	s := newTestStore(t)
	workConfig := map[string]any{"host": "imap.example.com", "port": 993, "username": "alice@example.com"}
	work, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, " Work Mail ", workConfig)
	if err != nil {
		t.Fatalf("CreateExternalAccount(work) error: %v", err)
	}
	if work.Label != "Work Mail" {
		t.Fatalf("work label = %q, want %q", work.Label, "Work Mail")
	}
	if !work.Enabled {
		t.Fatal("expected created external account to be enabled")
	}
	personal, err := s.CreateExternalAccount(SpherePrivate, ExternalProviderGmail, "Personal Gmail", map[string]any{"username": "bob@gmail.com", "token_file": "gmail-personal.json"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(personal) error: %v", err)
	}
	gotWork, err := s.GetExternalAccount(work.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(work) error: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal([]byte(gotWork.ConfigJSON), &decoded); err != nil {
		t.Fatalf("unmarshal config_json: %v", err)
	}
	if decoded["host"] != "imap.example.com" {
		t.Fatalf("config host = %v, want imap.example.com", decoded["host"])
	}
	workAccounts, err := s.ListExternalAccounts(SphereWork)
	if err != nil {
		t.Fatalf("ListExternalAccounts(work) error: %v", err)
	}
	if len(workAccounts) != 1 || workAccounts[0].ID != work.ID {
		t.Fatalf("ListExternalAccounts(work) = %+v, want only work account", workAccounts)
	}
	gmailAccounts, err := s.ListExternalAccountsByProvider(ExternalProviderGmail)
	if err != nil {
		t.Fatalf("ListExternalAccountsByProvider(gmail) error: %v", err)
	}
	if len(gmailAccounts) != 1 || gmailAccounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccountsByProvider(gmail) = %+v, want personal account", gmailAccounts)
	}
	updatedLabel := "Personal Gmail Primary"
	disabled := false
	if err := s.UpdateExternalAccount(personal.ID, ExternalAccountUpdate{AccountName: &updatedLabel, Config: map[string]any{"username": "bob@gmail.com", "token_path": "/tmp/tokens/personal.json"}, Enabled: &disabled}); err != nil {
		t.Fatalf("UpdateExternalAccount() error: %v", err)
	}
	gotPersonal, err := s.GetExternalAccount(personal.ID)
	if err != nil {
		t.Fatalf("GetExternalAccount(personal) error: %v", err)
	}
	if gotPersonal.Label != updatedLabel {
		t.Fatalf("updated label = %q, want %q", gotPersonal.Label, updatedLabel)
	}
	if gotPersonal.Enabled {
		t.Fatal("expected updated external account to be disabled")
	}
	if err := s.DeleteExternalAccount(work.ID); err != nil {
		t.Fatalf("DeleteExternalAccount(work) error: %v", err)
	}
	accounts, err := s.ListExternalAccounts("")
	if err != nil {
		t.Fatalf("ListExternalAccounts(all) error: %v", err)
	}
	if len(accounts) != 1 || accounts[0].ID != personal.ID {
		t.Fatalf("ListExternalAccounts(all) = %+v, want only personal account", accounts)
	}
}

func TestExternalAccountStoreRejectsInvalidConfigAndIdentity(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.CreateExternalAccount("", ExternalProviderGmail, "Mail", nil); err == nil {
		t.Fatal("expected missing sphere error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, "smtp", "Mail", nil); err == nil {
		t.Fatal("expected unsupported provider error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "", nil); err == nil {
		t.Fatal("expected missing label error")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"password": "secret"}); err == nil {
		t.Fatal("expected password config rejection")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderExchangeEWS, "Mail", map[string]any{"legacy_helpy_env_var": "HELPY_IMAP_PASSWORD_TUGRAZ"}); err == nil {
		t.Fatal("expected legacy env var config rejection")
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"oauth_token": "raw-token"}); err == nil {
		t.Fatal("expected token config rejection")
	}
	first, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "mail@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(first) error: %v", err)
	}
	if _, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Mail", map[string]any{"username": "dupe@example.com"}); err == nil {
		t.Fatal("expected duplicate account identity rejection")
	}
	badSphere := "office"
	if err := s.UpdateExternalAccount(first.ID, ExternalAccountUpdate{Sphere: &badSphere}); err == nil {
		t.Fatal("expected invalid update sphere error")
	}
}

func TestExternalAccountCredentialHelpers(t *testing.T) {
	envVar := ExternalAccountPasswordEnvVar(ExternalProviderGoogleCalendar, "Work Calendar")
	if envVar != "SLOPPY_GOOGLE_CALENDAR_PASSWORD_WORK_CALENDAR" {
		t.Fatalf("ExternalAccountPasswordEnvVar() = %q", envVar)
	}
	tokenPath := ExternalAccountTokenPath("/home/test/.config/slopshell", ExternalProviderGmail, "Work Gmail")
	wantPath := filepath.Join("/home/test/.config/slopshell", "tokens", "gmail_work_gmail.json")
	if tokenPath != wantPath {
		t.Fatalf("ExternalAccountTokenPath() = %q, want %q", tokenPath, wantPath)
	}
}

func TestResolveExternalAccountPasswordUsesEnvFirstAndCaches(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{"host": "imap.example.com", "username": "alice@example.com", "credential_ref": "bw://work-email-imap"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	envVar := ExternalAccountPasswordEnvVar(account.Provider, account.Label)
	s.SetExternalAccountLookupEnv(func(key string) (string, bool) {
		if key != envVar {
			t.Fatalf("env lookup key = %q, want %q", key, envVar)
		}
		return "env-secret", true
	})
	commandCalls := 0
	s.SetExternalAccountCommandRunner(func(_ context.Context, name string, args ...string) (string, error) {
		commandCalls++
		t.Fatalf("unexpected credential command: %s %v", name, args)
		return "", nil
	})
	password, source, err := s.ResolveExternalAccountPassword(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPassword() error: %v", err)
	}
	if password != "env-secret" {
		t.Fatalf("ResolveExternalAccountPassword() password = %q, want env-secret", password)
	}
	if source != ExternalAccountCredentialSourceEnv {
		t.Fatalf("ResolveExternalAccountPassword() source = %q, want %q", source, ExternalAccountCredentialSourceEnv)
	}
	if commandCalls != 0 {
		t.Fatalf("credential command calls = %d, want 0", commandCalls)
	}
	s.SetExternalAccountLookupEnv(func(string) (string, bool) {
		return "", false
	})
	password, source, err = s.ResolveExternalAccountPassword(context.Background(), account.ID)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPassword() cached error: %v", err)
	}
	if password != "env-secret" {
		t.Fatalf("cached password = %q, want env-secret", password)
	}
	if source != ExternalAccountCredentialSourceEnv {
		t.Fatalf("cached source = %q, want %q", source, ExternalAccountCredentialSourceEnv)
	}
	if commandCalls != 0 {
		t.Fatalf("credential command calls after cache = %d, want 0", commandCalls)
	}
}

func TestResolveExternalAccountPasswordFallsBackToBitwardenAndCaches(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{"host": "imap.example.com", "username": "alice@example.com", "credential_ref": "bw://work-email-imap"})
	if err != nil {
		t.Fatalf("CreateExternalAccount() error: %v", err)
	}
	s.SetExternalAccountLookupEnv(func(string) (string, bool) {
		return "", false
	})
	commandCalls := 0
	s.SetExternalAccountCommandRunner(func(_ context.Context, name string, args ...string) (string, error) {
		commandCalls++
		if name != "bw" {
			t.Fatalf("command name = %q, want bw", name)
		}
		if len(args) != 3 || args[0] != "get" || args[1] != "password" || args[2] != "work-email-imap" {
			t.Fatalf("command args = %#v, want bw get password work-email-imap", args)
		}
		return "bitwarden-secret\n", nil
	})
	password, source, err := s.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPasswordForAccount() error: %v", err)
	}
	if password != "bitwarden-secret" {
		t.Fatalf("password = %q, want bitwarden-secret", password)
	}
	if source != ExternalAccountCredentialSourceBitwarden {
		t.Fatalf("source = %q, want %q", source, ExternalAccountCredentialSourceBitwarden)
	}
	if commandCalls != 1 {
		t.Fatalf("credential command calls = %d, want 1", commandCalls)
	}
	password, source, err = s.ResolveExternalAccountPasswordForAccount(context.Background(), account)
	if err != nil {
		t.Fatalf("ResolveExternalAccountPasswordForAccount() cached error: %v", err)
	}
	if password != "bitwarden-secret" {
		t.Fatalf("cached password = %q, want bitwarden-secret", password)
	}
	if source != ExternalAccountCredentialSourceBitwarden {
		t.Fatalf("cached source = %q, want %q", source, ExternalAccountCredentialSourceBitwarden)
	}
	if commandCalls != 1 {
		t.Fatalf("credential command calls after cache = %d, want 1", commandCalls)
	}
}

func TestResolveExternalAccountPasswordRejectsMissingOrUnsupportedCredentialConfig(t *testing.T) {
	s := newTestStore(t)
	s.SetExternalAccountLookupEnv(func(string) (string, bool) {
		return "", false
	})
	missingAccount, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Work Mail", map[string]any{"host": "imap.example.com", "username": "alice@example.com"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(missing) error: %v", err)
	}
	if _, _, err := s.ResolveExternalAccountPassword(context.Background(), missingAccount.ID); !errors.Is(err, ErrExternalAccountPasswordUnavailable) {
		t.Fatalf("ResolveExternalAccountPassword(missing) error = %v, want %v", err, ErrExternalAccountPasswordUnavailable)
	}
	unsupportedAccount, err := s.CreateExternalAccount(SphereWork, ExternalProviderIMAP, "Other Mail", map[string]any{"host": "imap.example.com", "username": "bob@example.com", "credential_ref": "vault://other-mail"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(unsupported) error: %v", err)
	}
	if _, _, err := s.ResolveExternalAccountPassword(context.Background(), unsupportedAccount.ID); err == nil || !strings.Contains(err.Error(), `unsupported credential_ref "vault://other-mail"`) {
		t.Fatalf("ResolveExternalAccountPassword(unsupported) error = %v, want unsupported credential_ref", err)
	}
}

func TestExternalBindingStoreCRUDAndQueries(t *testing.T) {
	s := newTestStore(t)
	account, err := s.CreateExternalAccount(SphereWork, ExternalProviderGmail, "Work Gmail", map[string]any{"username": "alice@example.com", "token_file": "gmail-work.json"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(work) error: %v", err)
	}
	otherAccount, err := s.CreateExternalAccount(SpherePrivate, ExternalProviderTodoist, "Personal Todoist", map[string]any{"username": "alice"})
	if err != nil {
		t.Fatalf("CreateExternalAccount(todoist) error: %v", err)
	}
	item, err := s.CreateItem("Follow up", ItemOptions{})
	if err != nil {
		t.Fatalf("CreateItem() error: %v", err)
	}
	title := "Imported thread"
	artifact, err := s.CreateArtifact(ArtifactKindEmail, nil, nil, &title, nil)
	if err != nil {
		t.Fatalf("CreateArtifact() error: %v", err)
	}
	containerRef := "INBOX/Work"
	remoteUpdatedAt := "2026-03-08T12:00:00Z"
	created, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: " GMAIL ", ObjectType: " Email ", RemoteID: " msg-1 ", ItemID: &item.ID, ArtifactID: &artifact.ID, ContainerRef: &containerRef, RemoteUpdatedAt: &remoteUpdatedAt})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(create) error: %v", err)
	}
	if created.ID == 0 {
		t.Fatal("expected created binding ID")
	}
	if created.Provider != ExternalProviderGmail {
		t.Fatalf("binding provider = %q, want %q", created.Provider, ExternalProviderGmail)
	}
	if created.ObjectType != "email" {
		t.Fatalf("binding object_type = %q, want email", created.ObjectType)
	}
	if created.RemoteID != "msg-1" {
		t.Fatalf("binding remote_id = %q, want msg-1", created.RemoteID)
	}
	if created.ItemID == nil || *created.ItemID != item.ID {
		t.Fatalf("binding item_id = %v, want %d", created.ItemID, item.ID)
	}
	if created.ArtifactID == nil || *created.ArtifactID != artifact.ID {
		t.Fatalf("binding artifact_id = %v, want %d", created.ArtifactID, artifact.ID)
	}
	if created.ContainerRef == nil || *created.ContainerRef != containerRef {
		t.Fatalf("binding container_ref = %v, want %q", created.ContainerRef, containerRef)
	}
	if created.RemoteUpdatedAt == nil || *created.RemoteUpdatedAt != "2026-03-08T12:00:00Z" {
		t.Fatalf("binding remote_updated_at = %v, want normalized timestamp", created.RemoteUpdatedAt)
	}
	if created.LastSyncedAt == "" {
		t.Fatal("expected last_synced_at")
	}
	got, err := s.GetBindingByRemote(account.ID, ExternalProviderGmail, "email", "msg-1")
	if err != nil {
		t.Fatalf("GetBindingByRemote() error: %v", err)
	}
	if got.ID != created.ID {
		t.Fatalf("GetBindingByRemote() id = %d, want %d", got.ID, created.ID)
	}
	latestRemoteAt, err := s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(created) error: %v", err)
	}
	if latestRemoteAt == nil || *latestRemoteAt != "2026-03-08T12:00:00Z" {
		t.Fatalf("LatestBindingRemoteUpdatedAt(created) = %v, want 2026-03-08T12:00:00Z", latestRemoteAt)
	}
	updatedRemoteAt := "2026-03-08T13:15:00Z"
	updated, err := s.UpsertExternalBinding(ExternalBinding{AccountID: account.ID, Provider: ExternalProviderGmail, ObjectType: "email", RemoteID: "msg-1", ItemID: &item.ID, ContainerRef: &containerRef, RemoteUpdatedAt: &updatedRemoteAt})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(update) error: %v", err)
	}
	if updated.ID != created.ID {
		t.Fatalf("updated binding ID = %d, want %d", updated.ID, created.ID)
	}
	if updated.ArtifactID != nil {
		t.Fatalf("updated binding artifact_id = %v, want nil after update", updated.ArtifactID)
	}
	if updated.RemoteUpdatedAt == nil || *updated.RemoteUpdatedAt != updatedRemoteAt {
		t.Fatalf("updated remote_updated_at = %v, want %q", updated.RemoteUpdatedAt, updatedRemoteAt)
	}
	if updated.LastSyncedAt == "" {
		t.Fatal("expected updated last_synced_at")
	}
	latestRemoteAt, err = s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(updated) error: %v", err)
	}
	if latestRemoteAt == nil || *latestRemoteAt != updatedRemoteAt {
		t.Fatalf("LatestBindingRemoteUpdatedAt(updated) = %v, want %q", latestRemoteAt, updatedRemoteAt)
	}
	otherRemoteAt := "2026-03-08T08:30:00Z"
	second, err := s.UpsertExternalBinding(ExternalBinding{AccountID: otherAccount.ID, Provider: ExternalProviderTodoist, ObjectType: "task", RemoteID: "task-7", ItemID: &item.ID, ArtifactID: &artifact.ID, RemoteUpdatedAt: &otherRemoteAt})
	if err != nil {
		t.Fatalf("UpsertExternalBinding(second) error: %v", err)
	}
	itemBindings, err := s.GetBindingsByItem(item.ID)
	if err != nil {
		t.Fatalf("GetBindingsByItem() error: %v", err)
	}
	if len(itemBindings) != 2 {
		t.Fatalf("GetBindingsByItem() len = %d, want 2", len(itemBindings))
	}
	if itemBindings[0].ID != updated.ID || itemBindings[1].ID != second.ID {
		t.Fatalf("GetBindingsByItem() order = %+v", itemBindings)
	}
	artifactBindings, err := s.GetBindingsByArtifact(artifact.ID)
	if err != nil {
		t.Fatalf("GetBindingsByArtifact() error: %v", err)
	}
	if len(artifactBindings) != 1 || artifactBindings[0].ID != second.ID {
		t.Fatalf("GetBindingsByArtifact() = %+v, want only second binding", artifactBindings)
	}
	oldSync := "2026-03-08T09:00:00Z"
	if _, err := s.DB().Exec(`UPDATE external_bindings SET last_synced_at = ? WHERE id = ?`, oldSync, updated.ID); err != nil {
		t.Fatalf("seed old last_synced_at: %v", err)
	}
	stale, err := s.ListStaleBindings(ExternalProviderGmail, time.Date(2026, time.March, 8, 10, 0, 0, 0, time.UTC))
	if err != nil {
		t.Fatalf("ListStaleBindings() error: %v", err)
	}
	if len(stale) != 1 || stale[0].ID != updated.ID {
		t.Fatalf("ListStaleBindings() = %+v, want updated binding only", stale)
	}
	if err := s.DeleteBinding(updated.ID); err != nil {
		t.Fatalf("DeleteBinding() error: %v", err)
	}
	if _, err := s.GetBindingByRemote(account.ID, ExternalProviderGmail, "email", "msg-1"); !errors.Is(err, sql.ErrNoRows) {
		t.Fatalf("GetBindingByRemote(deleted) error = %v, want sql.ErrNoRows", err)
	}
	latestRemoteAt, err = s.LatestBindingRemoteUpdatedAt(account.ID, ExternalProviderGmail, "email")
	if err != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(deleted) error: %v", err)
	}
	if latestRemoteAt != nil {
		t.Fatalf("LatestBindingRemoteUpdatedAt(deleted) = %v, want nil", latestRemoteAt)
	}
}
