package googleauth

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"golang.org/x/oauth2"
	"golang.org/x/oauth2/google"
)

const (
	ScopeGmailFull   = "https://mail.google.com/"
	ScopeContacts    = "https://www.googleapis.com/auth/contacts"
	ScopeCalendar    = "https://www.googleapis.com/auth/calendar"
	ScopeDrive       = "https://www.googleapis.com/auth/drive"
	ScopeDocs        = "https://www.googleapis.com/auth/documents"
	ScopeSheets      = "https://www.googleapis.com/auth/spreadsheets"
	ScopeTasks       = "https://www.googleapis.com/auth/tasks"

	// Deprecated aliases kept for compile compatibility
	ScopeGmailModify      = ScopeGmailFull
	ScopeContactsReadonly = ScopeContacts
	ScopeCalendarReadonly = "https://www.googleapis.com/auth/calendar.readonly"
)

var DefaultScopes = []string{
	ScopeGmailFull,
	ScopeContacts,
	ScopeCalendar,
	ScopeDrive,
	ScopeDocs,
	ScopeSheets,
	ScopeTasks,
}

type Session struct {
	config          *oauth2.Config
	token           *oauth2.Token
	credentialsPath string
	tokenPath       string
	mu              sync.Mutex
}

func DefaultConfigDir() string {
	home, err := os.UserHomeDir()
	if err != nil || strings.TrimSpace(home) == "" {
		return ".sloppy"
	}
	return filepath.Join(home, ".config", "sloppy")
}

func DefaultCredentialsPath() string {
	return filepath.Join(DefaultConfigDir(), "gmail_credentials.json")
}

func DefaultTokenPath() string {
	return filepath.Join(DefaultConfigDir(), "gmail_token.json")
}

func New(credentialsPath, tokenPath string, scopes []string) (*Session, error) {
	if strings.TrimSpace(credentialsPath) == "" {
		credentialsPath = DefaultCredentialsPath()
	}
	if strings.TrimSpace(tokenPath) == "" {
		tokenPath = DefaultTokenPath()
	}
	if len(scopes) == 0 {
		scopes = append([]string(nil), DefaultScopes...)
	}

	credBytes, err := os.ReadFile(credentialsPath)
	if err != nil {
		return nil, fmt.Errorf("configure Google credentials at %s first: %w", credentialsPath, err)
	}
	config, err := google.ConfigFromJSON(credBytes, scopes...)
	if err != nil {
		return nil, fmt.Errorf("parse Google credentials: %w", err)
	}

	session := &Session{
		config:          config,
		credentialsPath: credentialsPath,
		tokenPath:       tokenPath,
	}
	if tokenBytes, err := os.ReadFile(tokenPath); err == nil {
		var token oauth2.Token
		if json.Unmarshal(tokenBytes, &token) == nil {
			session.token = &token
		}
	}
	return session, nil
}

func (s *Session) CredentialsPath() string {
	if s == nil {
		return ""
	}
	return s.credentialsPath
}

func (s *Session) TokenPath() string {
	if s == nil {
		return ""
	}
	return s.tokenPath
}

func (s *Session) GetAuthURL() string {
	if s == nil || s.config == nil {
		return ""
	}
	return s.config.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
}

func (s *Session) GetAuthURLWithRedirect(redirectURI string) string {
	if s == nil || s.config == nil {
		return ""
	}
	cfg := *s.config
	cfg.RedirectURL = redirectURI
	return cfg.AuthCodeURL("state-token", oauth2.AccessTypeOffline)
}

func (s *Session) ExchangeCodeWithRedirect(ctx context.Context, code, redirectURI string) error {
	if s == nil || s.config == nil {
		return fmt.Errorf("google auth is not configured")
	}
	cfg := *s.config
	cfg.RedirectURL = redirectURI
	token, err := cfg.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange Google auth code: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	return s.persistLocked(token)
}

func (s *Session) ExchangeCode(ctx context.Context, code string) error {
	if s == nil || s.config == nil {
		return fmt.Errorf("google auth is not configured")
	}
	token, err := s.config.Exchange(ctx, code)
	if err != nil {
		return fmt.Errorf("exchange Google auth code: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.token = token
	return s.persistLocked(token)
}

func (s *Session) TokenSource(ctx context.Context) (oauth2.TokenSource, error) {
	if s == nil || s.config == nil {
		return nil, fmt.Errorf("google auth is not configured")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.token == nil {
		return nil, fmt.Errorf("google is not authenticated; token file %s is missing", s.tokenPath)
	}
	tokenSource := s.config.TokenSource(ctx, s.token)
	newToken, err := tokenSource.Token()
	if err != nil {
		return nil, fmt.Errorf("refresh Google token: %w", err)
	}
	if newToken.AccessToken != s.token.AccessToken || newToken.Expiry != s.token.Expiry {
		s.token = newToken
		if err := s.persistLocked(newToken); err != nil {
			return nil, err
		}
	}
	return tokenSource, nil
}

func (s *Session) persistLocked(token *oauth2.Token) error {
	if err := os.MkdirAll(filepath.Dir(s.tokenPath), 0o755); err != nil {
		return err
	}
	tokenBytes, err := json.MarshalIndent(token, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(s.tokenPath, tokenBytes, 0o600)
}
