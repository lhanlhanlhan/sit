package protocol

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestInstructionRoundTrip_MillisDeadline(t *testing.T) {
	in := Instruction{Kind: KindShell, Command: "echo hi", TimeoutSec: 30, Deadline: 1782269000000}
	env, err := NewInstruction(in)
	if err != nil {
		t.Fatalf("NewInstruction: %v", err)
	}
	if env.V != ProtocolVersion || env.Type != TypeInstruction || env.ID == "" || env.TS == 0 {
		t.Fatalf("envelope header wrong: %+v", env)
	}
	b, err := Encode(env)
	if err != nil {
		t.Fatalf("Encode: %v", err)
	}
	dec, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, err := dec.AsInstruction()
	if err != nil {
		t.Fatalf("AsInstruction: %v", err)
	}
	if got.Deadline != in.Deadline {
		t.Errorf("deadline ms not preserved: got %d want %d", got.Deadline, in.Deadline)
	}
	if got.Command != in.Command || got.TimeoutSec != in.TimeoutSec {
		t.Errorf("instruction mismatch: %+v", got)
	}
}

func TestNotificationRoundTrip_DualStackAddrs(t *testing.T) {
	n := Notification{
		Kind:   KindRegister,
		NodeID: "01HXTESTULID",
		OS:     "darwin", Arch: "arm64", Version: "sit/0.1.0",
		Addrs: []Addr{
			{IP: "192.168.1.10", Family: "v4", Iface: "en0", Scope: "private"},
			{IP: "2001:db8::1", Family: "v6", Iface: "en0", Scope: "global"},
			{IP: "fe80::1", Family: "v6", Iface: "en0", Scope: "link"},
		},
	}
	env, err := NewNotification(n)
	if err != nil {
		t.Fatalf("NewNotification: %v", err)
	}
	b, _ := Encode(env)
	dec, err := Decode(b)
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	got, err := dec.AsNotification()
	if err != nil {
		t.Fatalf("AsNotification: %v", err)
	}
	if len(got.Addrs) != 3 {
		t.Fatalf("addrs length: got %d want 3", len(got.Addrs))
	}
	if got.Addrs[1].Family != "v6" || got.Addrs[1].IP != "2001:db8::1" {
		t.Errorf("v6 addr not preserved: %+v", got.Addrs[1])
	}
	if got.Addrs[0].Iface != "en0" || got.Addrs[2].Scope != "link" {
		t.Errorf("iface/scope not preserved: %+v", got.Addrs)
	}
}

func TestDecodeRejectsOversizeFrame(t *testing.T) {
	huge := strings.Repeat("x", MaxFrameBytes+1)
	if _, err := Decode([]byte(huge)); err != ErrFrameTooLarge {
		t.Fatalf("expected ErrFrameTooLarge, got %v", err)
	}
}

func TestDecodeRejectsVersionMismatch(t *testing.T) {
	bad, _ := json.Marshal(Envelope{V: 999, Type: TypeAck, ID: "x", TS: 1})
	_, err := Decode(bad)
	if err == nil || !strings.Contains(err.Error(), "unsupported version") {
		t.Fatalf("expected unsupported version error, got %v", err)
	}
}

func TestAsType_DispatchMismatch(t *testing.T) {
	env, _ := NewAck("ref-1")
	if _, err := env.AsInstruction(); err == nil {
		t.Error("AsInstruction on ack should error")
	}
	ack, err := env.AsAck()
	if err != nil {
		t.Fatalf("AsAck: %v", err)
	}
	if ack.RefID != "ref-1" {
		t.Errorf("ack ref_id: got %q want ref-1", ack.RefID)
	}
}

func TestNowMillisAndNewID(t *testing.T) {
	if NowMillis() < 1_700_000_000_000 {
		t.Error("NowMillis looks like seconds, not ms")
	}
	a, b := NewID(), NewID()
	if a == "" || a == b {
		t.Errorf("NewID not unique: %q %q", a, b)
	}
}
