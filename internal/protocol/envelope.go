// Package protocol defines the SIT wire contract shared by Manager and Node.
//
// All messages are JSON over WebSocket text frames. All time fields are
// unix milliseconds (int64). Message IDs are ULID strings.
package protocol

import (
	"crypto/rand"
	"time"

	"github.com/oklog/ulid/v2"
)

// ProtocolVersion is the current envelope version.
const ProtocolVersion = 1

// MaxFrameBytes is the hard upper bound on a single decoded frame (1 MiB).
// Decoders MUST reject larger frames before allocating, to protect
// memory-constrained (2-4 GB) embedded nodes.
const MaxFrameBytes = 1 << 20

// Message types carried in Envelope.Type.
const (
	TypeInstruction  = "instruction"
	TypeNotification = "notification"
	TypeAck          = "ack"
)

// NowMillis returns the current unix time in milliseconds.
func NowMillis() int64 {
	return time.Now().UnixMilli()
}

// NewID returns a fresh ULID string for use as a message ID.
func NewID() string {
	return ulid.MustNew(ulid.Now(), rand.Reader).String()
}
