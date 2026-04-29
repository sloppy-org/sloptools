package brain

import (
	"errors"
	"fmt"
)

type ErrorKind string

const (
	ErrorInvalidConfig   ErrorKind = "invalid_config"
	ErrorUnknownVault    ErrorKind = "unknown_vault"
	ErrorOutOfVault      ErrorKind = "out_of_vault"
	ErrorExcludedPath    ErrorKind = "excluded_path"
	ErrorUnsupportedLink ErrorKind = "unsupported_link"
)

type PathOp string

const (
	OpRead     PathOp = "read"
	OpList     PathOp = "list"
	OpIndex    PathOp = "index"
	OpMetadata PathOp = "metadata"
	OpLink     PathOp = "link"
)

type PathError struct {
	Kind   ErrorKind
	Op     PathOp
	Sphere Sphere
	Path   string
	Link   string
	Err    error
}

func (e *PathError) Error() string {
	msg := string(e.Kind)
	if e.Op != "" {
		msg += " during " + string(e.Op)
	}
	if e.Sphere != "" {
		msg += " for " + string(e.Sphere)
	}
	if e.Path != "" {
		msg += fmt.Sprintf(" path %q", e.Path)
	}
	if e.Link != "" {
		msg += fmt.Sprintf(" link %q", e.Link)
	}
	if e.Err != nil {
		msg += ": " + e.Err.Error()
	}
	return msg
}

func (e *PathError) Unwrap() error {
	return e.Err
}

func KindOf(err error) ErrorKind {
	var pathErr *PathError
	if errors.As(err, &pathErr) {
		return pathErr.Kind
	}
	return ""
}
