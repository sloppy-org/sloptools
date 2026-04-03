package googleauth

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"golang.org/x/oauth2"
)

func TestNewLoadsTokenFromDefaultFiles(t *testing.T) {
	configDir := t.TempDir()
	t.Setenv("HOME", configDir)
	tokenPath := filepath.Join(DefaultConfigDir(), "gmail_token.json")
	credentialsPath := filepath.Join(DefaultConfigDir(), "gmail_credentials.json")
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	if err := os.WriteFile(credentialsPath, []byte(`{"installed":{"client_id":"cid","client_secret":"secret","auth_uri":"https://example.com/auth","token_uri":"https://example.com/token","redirect_uris":["http://localhost"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(credentials): %v", err)
	}
	if err := os.WriteFile(tokenPath, []byte(`{"access_token":"a1","refresh_token":"r1","token_type":"Bearer","expiry":"2026-03-16T00:00:00Z"}`), 0o600); err != nil {
		t.Fatalf("WriteFile(token): %v", err)
	}
	session, err := New("", "", nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if got := session.CredentialsPath(); got != credentialsPath {
		t.Fatalf("CredentialsPath = %q, want %q", got, credentialsPath)
	}
	if got := session.TokenPath(); got != tokenPath {
		t.Fatalf("TokenPath = %q, want %q", got, tokenPath)
	}
	if session.token == nil || session.token.AccessToken != "a1" {
		t.Fatalf("loaded token = %#v", session.token)
	}
}

func TestTokenSourceRefreshesAndPersistsToken(t *testing.T) {
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Fatalf("ParseForm: %v", err)
		}
		if got := r.Form.Get("refresh_token"); got != "refresh-me" {
			t.Fatalf("refresh_token = %q, want refresh-me", got)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"fresh-token","token_type":"Bearer","expires_in":3600}`))
	}))
	defer tokenServer.Close()

	dir := t.TempDir()
	credentialsPath := filepath.Join(dir, "credentials.json")
	tokenPath := filepath.Join(dir, "token.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"installed":{"client_id":"cid","client_secret":"secret","auth_uri":"https://example.com/auth","token_uri":"https://example.com/token","redirect_uris":["http://localhost"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(credentials): %v", err)
	}
	expired := oauth2.Token{
		AccessToken:  "stale-token",
		RefreshToken: "refresh-me",
		TokenType:    "Bearer",
		Expiry:       time.Now().Add(-time.Hour),
	}
	tokenBytes, err := json.Marshal(expired)
	if err != nil {
		t.Fatalf("Marshal(token): %v", err)
	}
	if err := os.WriteFile(tokenPath, tokenBytes, 0o600); err != nil {
		t.Fatalf("WriteFile(token): %v", err)
	}

	session, err := New(credentialsPath, tokenPath, nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	session.config.Endpoint.TokenURL = tokenServer.URL

	tokenSource, err := session.TokenSource(context.Background())
	if err != nil {
		t.Fatalf("TokenSource() error: %v", err)
	}
	token, err := tokenSource.Token()
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if token.AccessToken != "fresh-token" {
		t.Fatalf("AccessToken = %q, want fresh-token", token.AccessToken)
	}
	persisted, err := os.ReadFile(tokenPath)
	if err != nil {
		t.Fatalf("ReadFile(token): %v", err)
	}
	if !strings.Contains(string(persisted), "fresh-token") {
		t.Fatalf("persisted token missing refreshed access token: %s", string(persisted))
	}
}

func TestTokenSourceRequiresToken(t *testing.T) {
	dir := t.TempDir()
	credentialsPath := filepath.Join(dir, "credentials.json")
	if err := os.WriteFile(credentialsPath, []byte(`{"installed":{"client_id":"cid","client_secret":"secret","auth_uri":"https://example.com/auth","token_uri":"https://example.com/token","redirect_uris":["http://localhost"]}}`), 0o600); err != nil {
		t.Fatalf("WriteFile(credentials): %v", err)
	}
	session, err := New(credentialsPath, filepath.Join(dir, "missing-token.json"), nil)
	if err != nil {
		t.Fatalf("New() error: %v", err)
	}
	if _, err := session.TokenSource(context.Background()); err == nil || !strings.Contains(err.Error(), "token file") {
		t.Fatalf("TokenSource() error = %v, want missing token file error", err)
	}
}
