package store

// Node represents a managed device. node_id is self-reported but validated by
// the handshake credential; display_name is editable on the Manager side.
type Node struct {
	NodeID      string `json:"node_id"`
	DisplayName string `json:"display_name"`
	OS          string `json:"os"`
	Arch        string `json:"arch"`
	Version     string `json:"version"`
	Hostname    string `json:"hostname"`
	AddrsJSON   string `json:"addrs_json"` // JSON-encoded []protocol.Addr
	Status      string `json:"status"`     // online | offline
	MCPEnabled  bool   `json:"mcp_enabled"`
	LastSeen    int64  `json:"last_seen"` // unix ms
	CreatedAt   int64  `json:"created_at"`
}

// Credential states.
const (
	CredActive  = "active"
	CredRevoked = "revoked"
)

// Credential is a node's long-term secret (stored hashed only).
type Credential struct {
	NodeID     string
	SecretHash string
	State      string // active | revoked
	IssuedAt   int64
	RevokedAt  int64
}

// Enroll token states.
const (
	EnrollUnused  = "unused"
	EnrollUsed    = "used"
	EnrollExpired = "expired"
)

// EnrollToken is a one-time node-enrollment token (stored hashed only).
type EnrollToken struct {
	TokenHash string
	State     string // unused | used | expired
	ExpiresAt int64
	CreatedAt int64
}

// Task kinds & states.
const (
	TaskShell      = "shell"
	TaskPredefined = "predefined"

	TaskQueued    = "queued"
	TaskSent      = "sent"
	TaskSucceeded = "succeeded"
	TaskFailed    = "failed"
	TaskExpired   = "expired"
	TaskTimeout   = "timeout"
)

// Task is a dispatched instruction plus its result. The tasks table doubles as
// the offline queue: state=queued rows for offline nodes are delivered on reconnect.
type Task struct {
	TaskID     string `json:"task_id"` // == instruction.id
	NodeID     string `json:"node_id"`
	Kind       string `json:"kind"`
	Command    string `json:"command,omitempty"`
	Name       string `json:"name,omitempty"`
	ArgsJSON   string `json:"args_json,omitempty"`
	State      string `json:"state"`
	CreatedAt  int64  `json:"created_at"`
	SentAt     int64  `json:"sent_at,omitempty"`
	FinishedAt int64  `json:"finished_at,omitempty"`
	Deadline   int64  `json:"deadline,omitempty"`
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	Truncated  bool   `json:"truncated"`
	DurationMS int64  `json:"duration_ms,omitempty"`
}

// Activity is one entry in a node's timeline.
type Activity struct {
	ID         int64  `json:"id"`
	NodeID     string `json:"node_id"`
	Type       string `json:"type"` // register|online|offline|event|task_sent|task_result
	DetailJSON string `json:"detail_json"`
	At         int64  `json:"at"`
}

// Admin is a REST login account (password stored hashed only).
type Admin struct {
	Username     string
	PasswordHash string
	CreatedAt    int64
}
