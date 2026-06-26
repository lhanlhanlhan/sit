package transport

import (
	"context"

	"github.com/sit/sit/internal/protocol"
)

// SessionInfo describes a live connection. NodeID is set on the Manager side
// after handshake authentication (authoritative identity), empty on the Node side.
type SessionInfo struct {
	NodeID     string // authenticated node id (manager side)
	RemoteAddr string
	ConnectedAt int64 // unix ms
}

// Conn is the pure-channel abstraction the business layers use. Implementations
// own a single websocket connection plus its read/write pumps; callers never
// touch the websocket directly.
type Conn interface {
	// Send enqueues an envelope for the write pump. Non-blocking unless the
	// send buffer is full; returns an error if the connection is closed.
	Send(ctx context.Context, e protocol.Envelope) error
	// Recv returns the channel of decoded inbound envelopes. Closed on disconnect.
	Recv() <-chan protocol.Envelope
	// Close shuts down the connection with a human-readable reason.
	Close(reason string) error
	// Info returns session metadata.
	Info() SessionInfo
	// Done is closed when the connection has fully terminated.
	Done() <-chan struct{}
}
