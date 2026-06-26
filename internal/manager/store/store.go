package store

import (
	"context"
	"errors"
)

// ErrNotFound is returned when a requested row does not exist.
var ErrNotFound = errors.New("store: not found")

// Store is the persistence boundary. Business code depends only on this
// interface so the SQLite backend can later be swapped for Postgres.
type Store interface {
	// Nodes
	UpsertNode(ctx context.Context, n Node) error
	GetNode(ctx context.Context, nodeID string) (Node, error)
	ListNodes(ctx context.Context, statusFilter, q string) ([]Node, error)
	SetDisplayName(ctx context.Context, nodeID, name string) error
	SetNodeStatus(ctx context.Context, nodeID, status string, lastSeen int64) error
	SetMCPEnabled(ctx context.Context, nodeID string, enabled bool) error
	DeleteNode(ctx context.Context, nodeID string) error

	// Credentials
	PutCredential(ctx context.Context, c Credential) error
	GetCredential(ctx context.Context, nodeID string) (Credential, error)
	RevokeCredential(ctx context.Context, nodeID string, at int64) error

	// Enroll tokens
	PutEnrollToken(ctx context.Context, t EnrollToken) error
	// ConsumeEnrollToken atomically marks an unused, unexpired token used and
	// returns true; otherwise returns false.
	ConsumeEnrollToken(ctx context.Context, tokenHash string, now int64) (bool, error)

	// Tasks
	CreateTask(ctx context.Context, t Task) error
	GetTask(ctx context.Context, taskID string) (Task, error)
	ListTasks(ctx context.Context, nodeID, stateFilter string, limit int) ([]Task, error)
	UpdateTaskState(ctx context.Context, taskID, state string, at int64) error
	CompleteTask(ctx context.Context, t Task) error
	// QueuedTasks returns queued tasks for a node ordered by created_at (offline queue).
	QueuedTasks(ctx context.Context, nodeID string) ([]Task, error)

	// Activities
	AppendActivity(ctx context.Context, a Activity) error
	ListActivities(ctx context.Context, nodeID string, limit int, before int64) ([]Activity, error)

	// Admins
	PutAdmin(ctx context.Context, a Admin) error
	GetAdmin(ctx context.Context, username string) (Admin, error)

	Close() error
}
