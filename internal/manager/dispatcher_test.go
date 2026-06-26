package manager

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

func newDispatcher(t *testing.T) (*Dispatcher, *Registry, store.Store) {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "disp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	r := NewRegistry()
	return NewDispatcher(s, r), r, s
}

func TestDispatcher_OfflineQueuesTask(t *testing.T) {
	d, _, s := newDispatcher(t)
	ctx := context.Background()
	task := store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell, Command: "echo hi", Deadline: protocol.NowMillis() + 60000}
	got, err := d.CreateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.TaskQueued {
		t.Errorf("offline task state: got %q want queued", got.State)
	}
	q, _ := s.QueuedTasks(ctx, "n1")
	if len(q) != 1 {
		t.Errorf("expected 1 queued, got %d", len(q))
	}
}

func TestDispatcher_OnlineSendsInstruction(t *testing.T) {
	d, r, s := newDispatcher(t)
	ctx := context.Background()
	conn := newFakeConn("n1")
	r.Add("n1", conn)

	task := store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell, Command: "echo hi"}
	got, err := d.CreateTask(ctx, task)
	if err != nil {
		t.Fatal(err)
	}
	if got.State != store.TaskSent {
		t.Errorf("online task state: got %q want sent", got.State)
	}
	if conn.sentCount() != 1 {
		t.Fatalf("expected 1 instruction sent, got %d", conn.sentCount())
	}
	env, _ := conn.lastSent()
	if env.ID != "t1" || env.Type != protocol.TypeInstruction {
		t.Errorf("instruction wrong: id=%q type=%q", env.ID, env.Type)
	}
	instr, _ := env.AsInstruction()
	if instr.Command != "echo hi" {
		t.Errorf("command not carried: %q", instr.Command)
	}
	// persisted as sent
	stored, _ := s.GetTask(ctx, "t1")
	if stored.State != store.TaskSent || stored.SentAt == 0 {
		t.Errorf("stored state: %+v", stored)
	}
}

func TestDispatcher_FlushQueueOnReconnect(t *testing.T) {
	d, r, _ := newDispatcher(t)
	ctx := context.Background()
	// create while offline
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell, Command: "a", Deadline: protocol.NowMillis() + 60000})
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t2", NodeID: "n1", Kind: store.TaskShell, Command: "b", Deadline: protocol.NowMillis() + 60000})
	// node connects
	conn := newFakeConn("n1")
	r.Add("n1", conn)
	if err := d.FlushQueue(ctx, "n1"); err != nil {
		t.Fatal(err)
	}
	if conn.sentCount() != 2 {
		t.Errorf("expected 2 flushed, got %d", conn.sentCount())
	}
}

func TestDispatcher_FlushExpiresStaleTasks(t *testing.T) {
	d, r, s := newDispatcher(t)
	ctx := context.Background()
	// deadline already in the past
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell, Command: "old", Deadline: protocol.NowMillis() - 1})
	conn := newFakeConn("n1")
	r.Add("n1", conn)
	_ = d.FlushQueue(ctx, "n1")
	if conn.sentCount() != 0 {
		t.Errorf("expired task should not be sent, sent=%d", conn.sentCount())
	}
	got, _ := s.GetTask(ctx, "t1")
	if got.State != store.TaskExpired {
		t.Errorf("state: got %q want expired", got.State)
	}
}

func TestDispatcher_HandleResultSucceeded(t *testing.T) {
	d, r, s := newDispatcher(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell, Command: "echo hi"})

	res := protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 0, Stdout: "hi\n", DurationMS: 12}
	if err := d.HandleResult(ctx, res); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(ctx, "t1")
	if got.State != store.TaskSucceeded || got.Stdout != "hi\n" {
		t.Errorf("result: %+v", got)
	}
}

func TestDispatcher_HandleResultFailedAndTimeout(t *testing.T) {
	d, r, s := newDispatcher(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell})
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t2", NodeID: "n1", Kind: store.TaskShell})

	_ = d.HandleResult(ctx, protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 3})
	_ = d.HandleResult(ctx, protocol.Notification{Kind: protocol.KindResult, RefID: "t2", TimedOut: true})

	g1, _ := s.GetTask(ctx, "t1")
	g2, _ := s.GetTask(ctx, "t2")
	if g1.State != store.TaskFailed {
		t.Errorf("t1: got %q want failed", g1.State)
	}
	if g2.State != store.TaskTimeout {
		t.Errorf("t2: got %q want timeout", g2.State)
	}
}

func TestDispatcher_DuplicateResultDropped(t *testing.T) {
	d, r, s := newDispatcher(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell})

	_ = d.HandleResult(ctx, protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 0, Stdout: "first"})
	// duplicate with different content must be ignored
	_ = d.HandleResult(ctx, protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 1, Stdout: "second"})
	got, _ := s.GetTask(ctx, "t1")
	if got.Stdout != "first" || got.State != store.TaskSucceeded {
		t.Errorf("dedup failed: %+v", got)
	}
}

func TestDispatcher_SyncWaiter(t *testing.T) {
	d, r, _ := newDispatcher(t)
	ctx := context.Background()
	r.Add("n1", newFakeConn("n1"))
	_, _ = d.CreateTask(ctx, store.Task{TaskID: "t1", NodeID: "n1", Kind: store.TaskShell})

	ch := d.Wait("t1")
	go func() {
		_ = d.HandleResult(ctx, protocol.Notification{Kind: protocol.KindResult, RefID: "t1", ExitCode: 0, Stdout: "done"})
	}()
	got := <-ch
	if got.Stdout != "done" || got.State != store.TaskSucceeded {
		t.Errorf("waiter got %+v", got)
	}
}
