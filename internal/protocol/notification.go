package protocol

// Notification kinds (Node → Manager). Frozen at these five for v1;
// result_chunk is reserved for future streaming.
const (
	KindRegister    = "register"
	KindHeartbeat   = "heartbeat"
	KindResult      = "result"
	KindEvent       = "event"
	KindResultChunk = "result_chunk" // reserved, v1 unused
)

// Addr is one network address of a Node (dual-stack, multi-IP aware).
type Addr struct {
	IP     string `json:"ip"`
	Family string `json:"family"` // v4 | v6
	Iface  string `json:"iface"`  // network interface name
	Scope  string `json:"scope"`  // global | private | link | loopback
}

// Notification is the generic Node → Node report payload. Kind selects which
// fields are meaningful. Identity fields (NodeID/Addrs) are self-reported and
// are NOT an authentication basis — the handshake credential is authoritative.
type Notification struct {
	Kind string `json:"kind"`

	// register
	NodeID   string `json:"node_id,omitempty"`
	Hostname string `json:"hostname,omitempty"`
	OS       string `json:"os,omitempty"`
	Arch     string `json:"arch,omitempty"`
	Version  string `json:"version,omitempty"`
	Addrs    []Addr `json:"addrs,omitempty"`

	// heartbeat
	UptimeSec int64     `json:"uptime_sec,omitempty"`
	Load      []float64 `json:"load,omitempty"`
	MemUsedMB int64     `json:"mem_used_mb,omitempty"`

	// result
	RefID      string `json:"ref_id,omitempty"` // -> instruction.id
	ExitCode   int    `json:"exit_code,omitempty"`
	Stdout     string `json:"stdout,omitempty"`
	Stderr     string `json:"stderr,omitempty"`
	DurationMS int64  `json:"duration_ms,omitempty"`
	Truncated  bool   `json:"truncated,omitempty"`
	TimedOut   bool   `json:"timed_out,omitempty"`

	// event
	Event  string `json:"event,omitempty"` // online | shutting_down | process_died ...
	Detail string `json:"detail,omitempty"`
}
