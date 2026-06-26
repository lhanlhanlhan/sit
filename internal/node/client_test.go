package node

import (
	"context"
	"testing"
	"time"

	"github.com/sit/sit/internal/protocol"
)

func TestReporter_RegisterEnumeratesAddrs(t *testing.T) {
	rp := NewReporter("n1", "sit/0.1.0")
	reg := rp.Register()
	if reg.Kind != protocol.KindRegister || reg.NodeID != "n1" {
		t.Fatalf("register: %+v", reg)
	}
	if reg.OS == "" || reg.Arch == "" {
		t.Errorf("os/arch not set: %+v", reg)
	}
	// Loopback always exists; at least one addr should be enumerated.
	if len(reg.Addrs) == 0 {
		t.Error("expected at least one local addr")
	}
	for _, a := range reg.Addrs {
		if a.Family != "v4" && a.Family != "v6" {
			t.Errorf("bad family: %+v", a)
		}
	}
}

func TestClient_RegisterAckExecuteResultFlow(t *testing.T) {
	conn := newFakeConn()
	rp := NewReporter("n1", "sit/0.1.0")
	ex := NewExecutor(nil)
	c := NewClient(conn, rp, ex, time.Hour) // long heartbeat: not under test

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan struct{})
	go func() { _ = c.Serve(ctx); close(done) }()

	// Deliver a shell instruction.
	instrEnv, _ := protocol.NewInstruction(protocol.Instruction{Kind: protocol.KindShell, Command: "echo hello", TimeoutSec: 10})
	instrEnv.ID = "task-1"
	conn.deliver(instrEnv)

	// Wait for the result to appear in sent frames.
	var gotAck, gotResult bool
	deadline := time.After(5 * time.Second)
	for !(gotAck && gotResult) {
		select {
		case <-deadline:
			t.Fatalf("timed out; sent=%d", len(conn.sentEnvelopes()))
		case <-time.After(20 * time.Millisecond):
		}
		for _, e := range conn.sentEnvelopes() {
			switch e.Type {
			case protocol.TypeAck:
				if ack, _ := e.AsAck(); ack.RefID == "task-1" {
					gotAck = true
				}
			case protocol.TypeNotification:
				if n, _ := e.AsNotification(); n.Kind == protocol.KindResult && n.RefID == "task-1" {
					gotResult = true
					if n.ExitCode != 0 {
						t.Errorf("result exit: %d", n.ExitCode)
					}
				}
			}
		}
	}

	// First two frames should be register + online event.
	sent := conn.sentEnvelopes()
	first, _ := sent[0].AsNotification()
	if first.Kind != protocol.KindRegister {
		t.Errorf("first frame should be register, got %q", first.Kind)
	}

	cancel()
	<-done
}
