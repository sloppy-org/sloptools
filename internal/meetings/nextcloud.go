package meetings

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// Nextcloud OCS share-permission bitmask values for the
// /ocs/v2.php/apps/files_sharing/api/v1/shares endpoint. They are
// documented at
// https://docs.nextcloud.com/server/latest/developer_manual/client_apis/OCS/ocs-share-api.html.
const (
	NextcloudPermRead   = 1
	NextcloudPermUpdate = 2
	NextcloudPermCreate = 4
	NextcloudPermDelete = 8
	NextcloudPermShare  = 16

	nextcloudShareTypePublicLink = 3
	nextcloudOCSSharePath        = "/ocs/v2.php/apps/files_sharing/api/v1/shares"
)

// PermissionsBitmask maps the issue-spec permission strings (`edit`,
// `read`, `comment`) to the OCS bitmask the Nextcloud share API
// expects. Edit grants read+update+create+delete+share for folders so
// recipients can tick checkboxes, comment, and add new lines (per issue
// #59); for files the create/delete bits are ignored server-side. An
// unknown value falls back to read-only.
func PermissionsBitmask(value string) int {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "edit":
		return NextcloudPermRead | NextcloudPermUpdate | NextcloudPermCreate | NextcloudPermDelete | NextcloudPermShare
	case "comment":
		return NextcloudPermRead | NextcloudPermShare
	default:
		return NextcloudPermRead
	}
}

// NextcloudConfig captures the credentials and sync-dir mapping that
// the share verbs need to talk to a real Nextcloud instance. It is
// loaded from the per-sphere `[meetings.<sphere>.nextcloud]` config
// block; missing fields disable live share creation, in which case the
// MCP verb requires the caller to supply a pre-existing share URL.
type NextcloudConfig struct {
	BaseURL      string
	User         string
	AppPassword  string
	LocalSyncDir string
}

// Configured reports whether enough fields are populated to attempt a
// live OCS request. The verbs use this to decide between a live API
// call and a metadata-only persist when the user has supplied a
// pre-baked share URL on the command line.
func (c NextcloudConfig) Configured() bool {
	return strings.TrimSpace(c.BaseURL) != "" &&
		strings.TrimSpace(c.User) != "" &&
		strings.TrimSpace(c.AppPassword) != ""
}

// NextcloudShareCreateOptions is the request payload for
// CreatePublicShare. ServerPath must be rooted at the Nextcloud user's
// files tree (begins with "/" and uses forward slashes).
type NextcloudShareCreateOptions struct {
	ServerPath  string
	Permissions int
	Password    string
	ExpireDate  string
	Label       string
}

// NextcloudShareRecord is the trimmed-down OCS share response that the
// drafter cares about. ID is the numeric share identifier required for
// later DELETE calls.
type NextcloudShareRecord struct {
	ID    string
	URL   string
	Token string
}

// NextcloudShareClient creates and revokes Nextcloud public-link shares
// via the OCS Share API. It is intentionally narrow: only the calls
// exercised by `meeting.share.create` and `meeting.share.revoke` are
// implemented. Helpy owns the full Nextcloud surface; this is the
// minimum sloptools needs to keep the meetings workflow self-contained.
type NextcloudShareClient interface {
	CreatePublicShare(ctx context.Context, opts NextcloudShareCreateOptions) (NextcloudShareRecord, error)
	DeleteShare(ctx context.Context, shareID string) error
	ResolveServerPath(absolutePath string) (string, error)
}

type httpNextcloudShareClient struct {
	cfg    NextcloudConfig
	client *http.Client
}

// NewNextcloudShareClient returns a real OCS-backed client. cfg.BaseURL
// is trimmed of trailing slashes; missing required fields produce a
// configuration error so callers can fall back to metadata-only mode.
func NewNextcloudShareClient(cfg NextcloudConfig) (NextcloudShareClient, error) {
	cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
	cfg.User = strings.TrimSpace(cfg.User)
	cfg.AppPassword = strings.TrimSpace(cfg.AppPassword)
	cfg.LocalSyncDir = strings.TrimSpace(cfg.LocalSyncDir)
	if cfg.BaseURL == "" {
		return nil, errors.New("nextcloud: base_url is required")
	}
	if cfg.User == "" {
		return nil, errors.New("nextcloud: user is required")
	}
	if cfg.AppPassword == "" {
		return nil, errors.New("nextcloud: app_password is required")
	}
	if cfg.LocalSyncDir != "" {
		cfg.LocalSyncDir = filepath.Clean(cfg.LocalSyncDir)
	}
	return &httpNextcloudShareClient{cfg: cfg, client: &http.Client{Timeout: 30 * time.Second}}, nil
}

func (c *httpNextcloudShareClient) ResolveServerPath(absolutePath string) (string, error) {
	clean := strings.TrimSpace(absolutePath)
	if clean == "" {
		return "", errors.New("nextcloud: path is required")
	}
	if c.cfg.LocalSyncDir == "" {
		return "", errors.New("nextcloud: local_sync_dir is not configured; cannot translate absolute path")
	}
	rel, err := filepath.Rel(c.cfg.LocalSyncDir, filepath.Clean(clean))
	if err != nil || strings.HasPrefix(rel, "..") {
		return "", fmt.Errorf("nextcloud: %q is not under local_sync_dir %q", clean, c.cfg.LocalSyncDir)
	}
	return path.Clean("/" + filepath.ToSlash(rel)), nil
}

func (c *httpNextcloudShareClient) CreatePublicShare(ctx context.Context, opts NextcloudShareCreateOptions) (NextcloudShareRecord, error) {
	if strings.TrimSpace(opts.ServerPath) == "" {
		return NextcloudShareRecord{}, errors.New("nextcloud: server_path is required")
	}
	perms := opts.Permissions
	if perms <= 0 {
		perms = NextcloudPermRead
	}
	form := url.Values{}
	form.Set("path", opts.ServerPath)
	form.Set("shareType", strconv.Itoa(nextcloudShareTypePublicLink))
	form.Set("permissions", strconv.Itoa(perms))
	if opts.Password != "" {
		form.Set("password", opts.Password)
	}
	if opts.ExpireDate != "" {
		form.Set("expireDate", opts.ExpireDate)
	}
	if opts.Label != "" {
		form.Set("label", opts.Label)
	}
	req, err := c.newOCSRequest(ctx, http.MethodPost, c.cfg.BaseURL+nextcloudOCSSharePath+"?format=json", strings.NewReader(form.Encode()))
	if err != nil {
		return NextcloudShareRecord{}, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	body, err := c.do(req)
	if err != nil {
		return NextcloudShareRecord{}, err
	}
	var env struct {
		OCS struct {
			Meta nextcloudOCSMeta `json:"meta"`
			Data struct {
				ID    json.Number `json:"id"`
				URL   string      `json:"url"`
				Token string      `json:"token"`
			} `json:"data"`
		} `json:"ocs"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return NextcloudShareRecord{}, fmt.Errorf("nextcloud: parse share create response: %w", err)
	}
	if !env.OCS.Meta.ok() {
		return NextcloudShareRecord{}, fmt.Errorf("nextcloud: share create failed: %s (%d)", env.OCS.Meta.Message, env.OCS.Meta.Statuscode)
	}
	return NextcloudShareRecord{ID: env.OCS.Data.ID.String(), URL: env.OCS.Data.URL, Token: env.OCS.Data.Token}, nil
}

func (c *httpNextcloudShareClient) DeleteShare(ctx context.Context, shareID string) error {
	id := strings.TrimSpace(shareID)
	if id == "" {
		return errors.New("nextcloud: share_id is required")
	}
	req, err := c.newOCSRequest(ctx, http.MethodDelete, c.cfg.BaseURL+nextcloudOCSSharePath+"/"+url.PathEscape(id)+"?format=json", nil)
	if err != nil {
		return err
	}
	body, err := c.do(req)
	if err != nil {
		return err
	}
	var env struct {
		OCS struct {
			Meta nextcloudOCSMeta `json:"meta"`
		} `json:"ocs"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return fmt.Errorf("nextcloud: parse share delete response: %w", err)
	}
	if !env.OCS.Meta.ok() {
		return fmt.Errorf("nextcloud: share delete failed: %s (%d)", env.OCS.Meta.Message, env.OCS.Meta.Statuscode)
	}
	return nil
}

type nextcloudOCSMeta struct {
	Status     string `json:"status"`
	Statuscode int    `json:"statuscode"`
	Message    string `json:"message"`
}

func (m nextcloudOCSMeta) ok() bool { return m.Statuscode == 100 || m.Statuscode == 200 }

func (c *httpNextcloudShareClient) newOCSRequest(ctx context.Context, method, target string, body io.Reader) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, method, target, body)
	if err != nil {
		return nil, fmt.Errorf("nextcloud: build %s %s: %w", method, target, err)
	}
	req.SetBasicAuth(c.cfg.User, c.cfg.AppPassword)
	req.Header.Set("OCS-APIRequest", "true")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "sloptools-meetings/1")
	return req, nil
}

func (c *httpNextcloudShareClient) do(req *http.Request) ([]byte, error) {
	resp, err := c.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("nextcloud: %s %s: %w", req.Method, req.URL.String(), err)
	}
	defer func() { _ = resp.Body.Close() }()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("nextcloud: %s %s: %s: %s", req.Method, req.URL.String(), resp.Status, strings.TrimSpace(string(body)))
	}
	return body, nil
}
