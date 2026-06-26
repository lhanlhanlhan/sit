package protocol

import (
	"encoding/json"
	"errors"
	"fmt"
)

// Envelope is the uniform wire frame. Payload is kept raw so the concrete type
// can be resolved by Type after a size/version check.
type Envelope struct {
	V       int             `json:"v"`
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	TS      int64           `json:"ts"` // unix ms
	Payload json.RawMessage `json:"payload"`
}

var (
	// ErrFrameTooLarge is returned when an encoded/decoded frame exceeds MaxFrameBytes.
	ErrFrameTooLarge = errors.New("protocol: frame exceeds max size")
	// ErrUnsupportedVersion is returned when Envelope.V does not match ProtocolVersion.
	ErrUnsupportedVersion = errors.New("protocol: unsupported version")
)

func newEnvelope(typ string, payload any) (Envelope, error) {
	raw, err := json.Marshal(payload)
	if err != nil {
		return Envelope{}, err
	}
	return Envelope{
		V:       ProtocolVersion,
		Type:    typ,
		ID:      NewID(),
		TS:      NowMillis(),
		Payload: raw,
	}, nil
}

// NewInstruction builds an instruction envelope with a fresh ID and timestamp.
func NewInstruction(p Instruction) (Envelope, error) {
	return newEnvelope(TypeInstruction, p)
}

// NewNotification builds a notification envelope with a fresh ID and timestamp.
func NewNotification(p Notification) (Envelope, error) {
	return newEnvelope(TypeNotification, p)
}

// NewAck builds an ack envelope referencing refID.
func NewAck(refID string) (Envelope, error) {
	return newEnvelope(TypeAck, Ack{RefID: refID})
}

// Encode serializes an envelope, enforcing the frame size limit.
func Encode(e Envelope) ([]byte, error) {
	b, err := json.Marshal(e)
	if err != nil {
		return nil, err
	}
	if len(b) > MaxFrameBytes {
		return nil, ErrFrameTooLarge
	}
	return b, nil
}

// Decode parses a frame: enforces the size limit before allocating the struct,
// then validates the protocol version.
func Decode(b []byte) (Envelope, error) {
	if len(b) > MaxFrameBytes {
		return Envelope{}, ErrFrameTooLarge
	}
	var e Envelope
	if err := json.Unmarshal(b, &e); err != nil {
		return Envelope{}, fmt.Errorf("protocol: decode: %w", err)
	}
	if e.V != ProtocolVersion {
		return Envelope{}, fmt.Errorf("%w: got %d want %d", ErrUnsupportedVersion, e.V, ProtocolVersion)
	}
	return e, nil
}

// AsInstruction decodes the payload as an Instruction. Errors if Type mismatches.
func (e Envelope) AsInstruction() (Instruction, error) {
	if e.Type != TypeInstruction {
		return Instruction{}, fmt.Errorf("protocol: not an instruction: %q", e.Type)
	}
	var p Instruction
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

// AsNotification decodes the payload as a Notification. Errors if Type mismatches.
func (e Envelope) AsNotification() (Notification, error) {
	if e.Type != TypeNotification {
		return Notification{}, fmt.Errorf("protocol: not a notification: %q", e.Type)
	}
	var p Notification
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}

// AsAck decodes the payload as an Ack. Errors if Type mismatches.
func (e Envelope) AsAck() (Ack, error) {
	if e.Type != TypeAck {
		return Ack{}, fmt.Errorf("protocol: not an ack: %q", e.Type)
	}
	var p Ack
	err := json.Unmarshal(e.Payload, &p)
	return p, err
}
