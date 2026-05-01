package meetings

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"strconv"
	"strings"
	"time"
)

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
