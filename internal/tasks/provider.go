// Package tasks defines the core task-management contract and the
// capability interfaces that specific backends (Google Tasks, Exchange
// EWS, etc.) may implement on top of it. The split mirrors the email
// package: the core Provider covers read and identity, and capability
// interfaces layer on mutation, completion toggling, and task-list
// management.
package tasks

import (
	"context"
	"errors"

	"github.com/sloppy-org/sloptools/internal/providerdata"
)

// ErrUnsupported signals that a Provider does not implement a requested
// capability. Callers should use errors.Is to detect it so that wrapped
// errors from downstream layers still match.
var ErrUnsupported = errors.New("tasks: provider does not support this capability")

// Provider is the minimum contract every task backend must satisfy: list
// the visible task containers, list tasks in one of them, fetch a single
// task, identify itself, and release any long-lived resources.
type Provider interface {
	ListTaskLists(ctx context.Context) ([]providerdata.TaskList, error)
	ListTasks(ctx context.Context, listID string) ([]providerdata.TaskItem, error)
	GetTask(ctx context.Context, listID, id string) (providerdata.TaskItem, error)
	ProviderName() string
	Close() error
}

// BulkLister exposes a provider-specific efficient path for fetching all
// visible tasks without iterating container-by-container. Callers should fall
// back to Provider.ListTasks when the provider does not implement it or returns
// ErrUnsupported.
type BulkLister interface {
	ListAllTasks(ctx context.Context) ([]providerdata.TaskItem, error)
}

// Mutator adds create/update/delete on individual tasks. Backends that
// only support a read-only view should omit this capability.
type Mutator interface {
	CreateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error)
	UpdateTask(ctx context.Context, listID string, t providerdata.TaskItem) (providerdata.TaskItem, error)
	DeleteTask(ctx context.Context, listID, id string) error
}

// Completer toggles the completed state on a task. Backends expose this
// separately because completion semantics (status + timestamp) differ
// enough from a generic update that a focused capability is clearer.
type Completer interface {
	CompleteTask(ctx context.Context, listID, id string) error
	UncompleteTask(ctx context.Context, listID, id string) error
}

// ListManager covers CRUD on the task containers themselves. Not every
// backend lets callers create or delete lists, so this is optional.
type ListManager interface {
	CreateTaskList(ctx context.Context, name string) (providerdata.TaskList, error)
	DeleteTaskList(ctx context.Context, id string) error
}
