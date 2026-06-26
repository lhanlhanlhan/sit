package protocol

// Instruction kinds.
const (
	KindPredefined = "predefined"
	KindShell      = "shell"
)

// Instruction is the Manager → Node command payload (Envelope.Type == TypeInstruction).
type Instruction struct {
	Kind string `json:"kind"` // predefined | shell

	// predefined: whitelisted command name + args.
	Name string         `json:"name,omitempty"`
	Args map[string]any `json:"args,omitempty"`

	// shell: arbitrary non-interactive command string (highest-risk surface).
	Command string `json:"command,omitempty"`

	TimeoutSec int   `json:"timeout_sec,omitempty"` // Node force-kills on overrun.
	Deadline   int64 `json:"deadline,omitempty"`    // unix ms; past => Node discards.

	// Stream is reserved (v1 unused): when set, Node reports output in
	// result_chunk notifications instead of a single result. See 01-protocol §3.
	Stream bool `json:"stream,omitempty"`
}
