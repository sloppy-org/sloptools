package meetings

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// ShareTargetKind reports whether the resolver picked the surrounding
// folder or a single Markdown file as the share target.
type ShareTargetKind string

const (
	// ShareTargetFolder means the share covers the meeting folder so
	// recipients see attachments and supplementary files alongside the
	// notes.
	ShareTargetFolder ShareTargetKind = "folder"

	// ShareTargetFile means the share covers a single Markdown file
	// because the meeting notes are stored as a loose file rather than
	// in a per-meeting folder.
	ShareTargetFile ShareTargetKind = "file"
)

const shareStateFilename = ".share.json"

// ShareTarget is the resolved meeting note location plus the kind that
// the share helper should hand to helpy `nextcloud_share_create`. The
// AbsolutePath is always under the configured meetings root; the
// VaultRelativePath is always rooted at the vault rather than the
// brain so it can be used directly in fallback templates.
type ShareTarget struct {
	Slug              string          `json:"slug"`
	Kind              ShareTargetKind `json:"kind"`
	AbsolutePath      string          `json:"absolute_path"`
	VaultRelativePath string          `json:"vault_relative_path,omitempty"`
	StatePath         string          `json:"state_path"`
}

// ShareState is the persisted record of the public share for one
// meeting. It is only updated by share.create and share.revoke; the
// drafter reads it to fill in the share URL before rendering email
// bodies.
type ShareState struct {
	Slug        string          `json:"slug"`
	Kind        ShareTargetKind `json:"kind"`
	ID          string          `json:"id,omitempty"`
	URL         string          `json:"url,omitempty"`
	Token       string          `json:"token,omitempty"`
	Permissions string          `json:"permissions,omitempty"`
	ExpiryDays  int             `json:"expiry_days,omitempty"`
	Password    bool            `json:"password,omitempty"`
	CreatedAt   string          `json:"created_at,omitempty"`
}

// ResolveShareTarget locates the share target for slug under
// meetingsRoot. A subfolder containing MEETING_NOTES.md wins; otherwise
// a `<slug>.md` file is looked up directly. Returns an error when
// neither layout is on disk.
func ResolveShareTarget(meetingsRoot, slug string) (ShareTarget, error) {
	root := strings.TrimSpace(meetingsRoot)
	if root == "" {
		return ShareTarget{}, errors.New("meetings_root is required")
	}
	clean := strings.TrimSpace(slug)
	if clean == "" {
		return ShareTarget{}, errors.New("slug is required")
	}
	folder := filepath.Join(root, clean)
	notesPath := filepath.Join(folder, "MEETING_NOTES.md")
	if info, err := os.Stat(folder); err == nil && info.IsDir() {
		if _, err := os.Stat(notesPath); err == nil {
			return ShareTarget{Slug: clean, Kind: ShareTargetFolder, AbsolutePath: folder, StatePath: filepath.Join(folder, shareStateFilename)}, nil
		}
	}
	loose := filepath.Join(root, clean+".md")
	if _, err := os.Stat(loose); err == nil {
		return ShareTarget{Slug: clean, Kind: ShareTargetFile, AbsolutePath: loose, StatePath: filepath.Join(root, "."+clean+".share.json")}, nil
	}
	return ShareTarget{}, fmt.Errorf("meeting %q not found under %s", clean, root)
}

// AttachVaultRelative annotates the target with its vault-relative
// path so the drafter can render fallback links without re-deriving
// the prefix. When vaultRoot is empty the annotation is skipped.
func (t ShareTarget) AttachVaultRelative(vaultRoot string) ShareTarget {
	clean := strings.TrimSpace(vaultRoot)
	if clean == "" {
		return t
	}
	rel, err := filepath.Rel(clean, t.AbsolutePath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return t
	}
	t.VaultRelativePath = filepath.ToSlash(rel)
	return t
}

// LoadShareState reads the persisted share record for target. Missing
// state files are not an error; the returned bool reports whether a
// state was found.
func LoadShareState(target ShareTarget) (ShareState, bool, error) {
	data, err := os.ReadFile(target.StatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return ShareState{}, false, nil
		}
		return ShareState{}, false, err
	}
	var state ShareState
	if err := json.Unmarshal(data, &state); err != nil {
		return ShareState{}, false, fmt.Errorf("read share state %s: %w", target.StatePath, err)
	}
	return state, true, nil
}

// WriteShareState persists state for target. The state file is created
// with 0o644 permissions so the user's vault sync clients can pick it
// up; callers should treat it as committed-style metadata, not secrets.
func WriteShareState(target ShareTarget, state ShareState) error {
	if err := os.MkdirAll(filepath.Dir(target.StatePath), 0o755); err != nil {
		return err
	}
	state.Slug = target.Slug
	state.Kind = target.Kind
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(target.StatePath, append(data, '\n'), 0o644)
}

// RemoveShareState deletes the persisted share record. Missing files
// are treated as no-ops because revocation is idempotent.
func RemoveShareState(target ShareTarget) error {
	if err := os.Remove(target.StatePath); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// ShareLink returns the URL the drafter should embed in an email. It
// prefers a recorded share state URL; failing that it renders the
// configured `url_template` with the `{vault_relative_path}` placeholder
// substituted; failing that it renders the per-sphere
// `note_link_fallback` template; and returns "" if nothing is
// configured. The returned bool reports whether the link should be
// considered live (a real share URL) rather than a fallback path.
func ShareLink(target ShareTarget, state ShareState, hasState bool, share ShareConfig) (string, bool) {
	if hasState && strings.TrimSpace(state.URL) != "" {
		return strings.TrimSpace(state.URL), true
	}
	rel := strings.TrimSpace(target.VaultRelativePath)
	if rendered := renderShareTemplate(share.URLTemplate, rel); rendered != "" {
		return rendered, true
	}
	if rendered := renderShareTemplate(share.NoteLinkFallback, rel); rendered != "" {
		return rendered, false
	}
	if rel != "" {
		return rel, false
	}
	return "", false
}

func renderShareTemplate(template, vaultRelativePath string) string {
	clean := strings.TrimSpace(template)
	if clean == "" {
		return ""
	}
	return strings.ReplaceAll(clean, "{vault_relative_path}", vaultRelativePath)
}

// NextcloudShareClientFactory builds a live NextcloudShareClient from a
// per-sphere config. Tests inject a fake factory; production wires it
// to NewNextcloudShareClient.
type NextcloudShareClientFactory func(NextcloudConfig) (NextcloudShareClient, error)

// liveShareTimeout caps blocking OCS calls so a stalled Nextcloud
// instance cannot wedge the meeting.share.* verbs.
const liveShareTimeout = 30 * time.Second

// ChooseSharePermissions resolves the permissions string for a share
// state from the request argument, the existing recorded value, and
// the per-sphere fallback. Empty inputs are treated as "use the next
// fallback in the chain"; the final default is "edit" so issue #59's
// recipient-edit workflow keeps working when no config is set.
func ChooseSharePermissions(existing, requested, fallback string) string {
	if v := strings.ToLower(strings.TrimSpace(requested)); v != "" {
		return v
	}
	if existing != "" {
		return existing
	}
	if v := strings.ToLower(strings.TrimSpace(fallback)); v != "" {
		return v
	}
	return "edit"
}

// RandomSharePassword returns a 24-character mixed-case alphanumeric
// password suitable for password-protected public shares. The fallback
// path uses the wall clock when the OS RNG is unavailable so the verb
// still produces a non-empty password rather than crashing.
func RandomSharePassword() string {
	const alphabet = "ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz23456789"
	buf := make([]byte, 24)
	if _, err := shareRandRead(buf); err != nil {
		return "share-" + strconv.FormatInt(time.Now().UnixNano(), 36)
	}
	out := make([]byte, len(buf))
	for i, b := range buf {
		out[i] = alphabet[int(b)%len(alphabet)]
	}
	return string(out)
}

var shareRandRead = func(buf []byte) (int, error) { return io.ReadFull(rand.Reader, buf) }

// CreateLiveShare resolves the OCS share request from target+state,
// invokes the factory-built client, and returns the resulting
// share record. It does not persist state; the caller writes the
// returned record into ShareState.
func CreateLiveShare(target ShareTarget, sphere SphereConfig, state ShareState, slug string, factory NextcloudShareClientFactory) (NextcloudShareRecord, error) {
	if !sphere.Nextcloud.Configured() {
		return NextcloudShareRecord{}, fmt.Errorf("nextcloud is not configured for sphere %q; either configure [meetings.%s.nextcloud] or pass an explicit url", sphere.Sphere, sphere.Sphere)
	}
	if factory == nil {
		factory = NewNextcloudShareClient
	}
	client, err := factory(sphere.Nextcloud)
	if err != nil {
		return NextcloudShareRecord{}, err
	}
	serverPath, err := client.ResolveServerPath(target.AbsolutePath)
	if err != nil {
		return NextcloudShareRecord{}, err
	}
	expireDate := ""
	if state.ExpiryDays > 0 {
		expireDate = time.Now().UTC().AddDate(0, 0, state.ExpiryDays).Format("2006-01-02")
	}
	password := ""
	if state.Password {
		password = RandomSharePassword()
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveShareTimeout)
	defer cancel()
	return client.CreatePublicShare(ctx, NextcloudShareCreateOptions{
		ServerPath:  serverPath,
		Permissions: PermissionsBitmask(state.Permissions),
		Password:    password,
		ExpireDate:  expireDate,
		Label:       "meeting:" + slug,
	})
}

// RevokeLiveShare deletes the recorded shareID via the OCS API. An
// empty shareID returns no-op success so callers can call this
// unconditionally.
func RevokeLiveShare(sphere SphereConfig, shareID string, factory NextcloudShareClientFactory) error {
	if strings.TrimSpace(shareID) == "" {
		return nil
	}
	if !sphere.Nextcloud.Configured() {
		return fmt.Errorf("nextcloud is not configured for sphere %q; cannot revoke share %q (configure [meetings.%s.nextcloud] or remove the recorded share id manually)", sphere.Sphere, shareID, sphere.Sphere)
	}
	if factory == nil {
		factory = NewNextcloudShareClient
	}
	client, err := factory(sphere.Nextcloud)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(context.Background(), liveShareTimeout)
	defer cancel()
	return client.DeleteShare(ctx, shareID)
}
