package manager

import (
	"testing"

	"github.com/sit/sit/internal/protocol"
)

func TestRegistry_AddOnlineConn(t *testing.T) {
	r := NewRegistry()
	c := newFakeConn("n1")
	r.Add("n1", c)
	if !r.IsOnline("n1") {
		t.Fatal("n1 should be online")
	}
	got, ok := r.Conn("n1")
	if !ok || got != c {
		t.Fatal("Conn lookup failed")
	}
	if r.IsOnline("ghost") {
		t.Error("ghost should not be online")
	}
}

func TestRegistry_AddReplacesAndClosesOld(t *testing.T) {
	r := NewRegistry()
	old := newFakeConn("n1")
	r.Add("n1", old)
	fresh := newFakeConn("n1")
	r.Add("n1", fresh)
	if !old.isClosed() {
		t.Error("old conn should be closed on replace")
	}
	got, _ := r.Conn("n1")
	if got != fresh {
		t.Error("registry should hold fresh conn")
	}
}

func TestRegistry_HeartbeatRefreshesLastSeen(t *testing.T) {
	r := NewRegistry()
	r.Add("n1", newFakeConn("n1"))
	hb := protocol.Notification{Kind: protocol.KindHeartbeat, UptimeSec: 3600, MemUsedMB: 1500}
	r.SetHeartbeat("n1", hb, 5000)
	ls, ok := r.LastSeen("n1")
	if !ok || ls != 5000 {
		t.Fatalf("last_seen: got %d ok=%v", ls, ok)
	}
	got, _ := r.LastHeartbeat("n1")
	if got.MemUsedMB != 1500 {
		t.Errorf("heartbeat metrics not stored: %+v", got)
	}
}

func TestRegistry_ReapStaleOnTimeout(t *testing.T) {
	r := NewRegistry()
	c := newFakeConn("n1")
	r.Add("n1", c)
	r.Touch("n1", 1000)
	// now is 1000 + 90001 ms -> stale
	reaped := r.ReapStale(1000 + OfflineTimeoutMS + 1)
	if len(reaped) != 1 || reaped[0] != "n1" {
		t.Fatalf("reaped: %v", reaped)
	}
	if r.IsOnline("n1") {
		t.Error("n1 should be offline after reap")
	}
	if !c.isClosed() {
		t.Error("conn should be closed on reap")
	}
}

func TestRegistry_ReapKeepsFresh(t *testing.T) {
	r := NewRegistry()
	r.Add("n1", newFakeConn("n1"))
	r.Touch("n1", 1000)
	// within timeout window
	reaped := r.ReapStale(1000 + OfflineTimeoutMS - 1)
	if len(reaped) != 0 {
		t.Errorf("should not reap fresh session: %v", reaped)
	}
	if !r.IsOnline("n1") {
		t.Error("n1 should still be online")
	}
}
