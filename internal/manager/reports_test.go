package manager

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

func newReports(t *testing.T) (*Reports, *Registry, *Dispatcher, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "rep.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRegistry()
	d := NewDispatcher(s, r)
	return NewReports(s, r, d), r, d, s
}

func TestReports_RegisterUsesAuthIdentityNotSelfReport(t *testing.T) {
	rp, r, _, s := newReports(t)
	ctx := context.Background()
	conn := newFakeConn("auth-node")
	r.Add("auth-node", conn)

	n := protocol.Notification{
		Kind:   protocol.KindRegister,
		NodeID: "i-am-lying", // self-reported; must be ignored
		OS:     "linux", Arch: "arm64", Version: "sit/0.1.0",
		Addrs: []protocol.Addr{{IP: "10.0.0.1", Family: "v4", Iface: "eth0", Scope: "private"}},
	}
	if err := rp.Handle(ctx, "auth-node", n); err != nil {
		t.Fatal(err)
	}
	// stored under authoritative id, not the self-reported one
	got, err := s.GetNode(ctx, "auth-node")
	if err != nil {
		t.Fatalf("node not stored under auth id: %v", err)
	}
	if got.OS != "linux" || got.Status != "online" {
		t.Errorf("node info: %+v", got)
	}
	if _, err := s.GetNode(ctx, "i-am-lying"); err != store.ErrNotFound {
		t.Errorf("self-reported id should not be stored: %v", err)
	}
	// activities recorded
	acts, _ := s.ListActivities(ctx, "auth-node", 10, 0)
	if len(acts) < 2 {
		t.Errorf("expected register+online activities, got %d", len(acts))
	}
}

func TestReports_HeartbeatRefreshes(t *testing.T) {
	rp, r, _, s := newReports(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_ = s.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]"})

	hb := protocol.Notification{Kind: protocol.KindHeartbeat, UptimeSec: 100, MemUsedMB: 800}
	if err := rp.Handle(ctx, "n1", hb); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetNode(ctx, "n1")
	if got.Status != "online" {
		t.Errorf("status: got %q want online", got.Status)
	}
	stored, ok := r.LastHeartbeat("n1")
	if !ok || stored.MemUsedMB != 800 {
		t.Errorf("heartbeat metrics not in registry: %+v", stored)
	}
}

func TestReports_ResultReachesDispatcher(t *testing.T) {
	rp, r, d, s := newReports(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell})

	res := protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 0, Stdout: "ok"}
	if err := rp.Handle(ctx, "n1", res); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(ctx, "t1")
	if got.State != store.TaskSucceeded || got.Stdout != "ok" {
		t.Errorf("result not applied: %+v", got)
	}
}

func TestReports_EventShuttingDownMarksOffline(t *testing.T) {
	rp, r, _, s := newReports(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_ = s.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]", Status: "online"})

	ev := protocol.Notification{Kind: protocol.KindEvent, Event: "shutting_down"}
	if err := rp.Handle(ctx, "n1", ev); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetNode(ctx, "n1")
	if got.Status != "offline" {
		t.Errorf("status: got %q want offline", got.Status)
	}
}
