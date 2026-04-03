package zotero

import (
	"net/url"
	"path/filepath"
	"strings"
)

func (r *Reader) AttachmentFilePath(attachment Attachment) string {
	path := strings.TrimSpace(attachment.Path)
	if path == "" {
		return ""
	}
	if strings.HasPrefix(strings.ToLower(path), "storage:") {
		relative := strings.TrimPrefix(path, "storage:")
		relative = strings.TrimLeft(relative, "/\\")
		if relative == "" {
			return ""
		}
		return filepath.Join(filepath.Dir(r.path), "storage", strings.TrimSpace(attachment.Key), filepath.FromSlash(relative))
	}
	if strings.HasPrefix(strings.ToLower(path), "file://") {
		parsed, err := url.Parse(path)
		if err != nil {
			return ""
		}
		return parsed.Path
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Clean(path)
}

func (r *Reader) AttachmentFileURL(attachment Attachment) string {
	path := r.AttachmentFilePath(attachment)
	if path == "" {
		return ""
	}
	return (&url.URL{Scheme: "file", Path: filepath.ToSlash(path)}).String()
}
