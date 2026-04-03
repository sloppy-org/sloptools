package email

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestNewGmailWithFilesLoadsAccountToken(t *testing.T) {
	dir := t.TempDir()
	credentialsPath := filepath.Join(dir, "credentials.json")
	tokenPath := filepath.Join(dir, "tokens", "work-gmail.json")

	credentials := `{
  "installed": {
    "client_id": "client-id.apps.googleusercontent.com",
    "project_id": "slopshell-test",
    "auth_uri": "https://accounts.google.com/o/oauth2/auth",
    "token_uri": "https://oauth2.googleapis.com/token",
    "client_secret": "secret",
    "redirect_uris": ["http://localhost"]
  }
}`
	if err := os.WriteFile(credentialsPath, []byte(credentials), 0o600); err != nil {
		t.Fatalf("WriteFile(credentials) error: %v", err)
	}
	if err := os.MkdirAll(filepath.Dir(tokenPath), 0o755); err != nil {
		t.Fatalf("MkdirAll(tokens) error: %v", err)
	}
	token := `{"access_token":"access-token","refresh_token":"refresh-token","token_type":"Bearer","expiry":"2030-01-01T00:00:00Z"}`
	if err := os.WriteFile(tokenPath, []byte(token), 0o600); err != nil {
		t.Fatalf("WriteFile(token) error: %v", err)
	}

	client, err := NewGmailWithFiles(credentialsPath, tokenPath)
	if err != nil {
		t.Fatalf("NewGmailWithFiles() error: %v", err)
	}
	if client.credentialsPath != credentialsPath {
		t.Fatalf("credentialsPath = %q, want %q", client.credentialsPath, credentialsPath)
	}
	if client.tokenPath != tokenPath {
		t.Fatalf("tokenPath = %q, want %q", client.tokenPath, tokenPath)
	}
	tokenSource, err := client.getTokenSource(context.Background())
	if err != nil {
		t.Fatalf("getTokenSource() error: %v", err)
	}
	tokenValue, err := tokenSource.Token()
	if err != nil {
		t.Fatalf("Token() error: %v", err)
	}
	if tokenValue.AccessToken != "access-token" {
		t.Fatalf("access token = %q, want access-token", tokenValue.AccessToken)
	}
}
