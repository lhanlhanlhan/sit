package store

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/sit/sit/internal/protocol"
)

func newTestStore(t *testing.T) *SQLiteStore {
	t.Helper()
	path := filepath.Join(t.TempDir(), "test.db")
	s, err := OpenSQLite(path)
	if err != nil {
		t.Fatalf("OpenSQLite: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestNode_UpsertGetList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	n := Node{NodeID: "n1", DisplayName: "box", OS: "linux", Status: "online",
		AddrsJSON: "[]", LastSeen: protocol.NowMillis(), CreatedAt: protocol.NowMillis()}
	if err := s.UpsertNode(ctx, n); err != nil {
		t.Fatalf("UpsertNode: %v", err)
	}
	got, err := s.GetNode(ctx, "n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if got.OS != "linux" || got.Status != "online" {
		t.Errorf("got %+v", got)
	}
	// upsert preserves created_at semantics and updates os
	n.OS = "darwin"
	_ = s.UpsertNode(ctx, n)
	got, _ = s.GetNode(ctx, "n1")
	if got.OS != "darwin" {
		t.Errorf("upsert did not update os: %+v", got)
	}
	list, err := s.ListNodes(ctx, "online", "")
	if err != nil || len(list) != 1 {
		t.Fatalf("ListNodes online: %v len=%d", err, len(list))
	}
	if l, _ := s.ListNodes(ctx, "offline", ""); len(l) != 0 {
		t.Errorf("expected 0 offline, got %d", len(l))
	}
}

func TestNode_NotFound(t *testing.T) {
	s := newTestStore(t)
	if _, err := s.GetNode(context.Background(), "nope"); err != ErrNotFound {
		t.Fatalf("got %v want ErrNotFound", err)
	}
}

func TestNode_RenameStatusMCPDelete(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	_ = s.UpsertNode(ctx, Node{NodeID: "n1", AddrsJSON: "[]"})
	if err := s.SetDisplayName(ctx, "n1", "My Mac"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetMCPEnabled(ctx, "n1", true); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeStatus(ctx, "n1", "offline", 123); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetNode(ctx, "n1")
	if got.DisplayName != "My Mac" || !got.MCPEnabled || got.Status != "offline" || got.LastSeen != 123 {
		t.Errorf("got %+v", got)
	}
	if err := s.SetDisplayName(ctx, "ghost", "x"); err != ErrNotFound {
		t.Errorf("rename missing node: got %v want ErrNotFound", err)
	}
	if err := s.DeleteNode(ctx, "n1"); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(ctx, "n1"); err != ErrNotFound {
		t.Errorf("after delete: got %v", err)
	}
}

func TestCredential_PutGetRevoke(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	c := Credential{NodeID: "n1", SecretHash: "deadbeef", State: CredActive, IssuedAt: 100}
	if err := s.PutCredential(ctx, c); err != nil {
		t.Fatal(err)
	}
	got, err := s.GetCredential(ctx, "n1")
	if err != nil || got.SecretHash != "deadbeef" || got.State != CredActive {
		t.Fatalf("got %+v err %v", got, err)
	}
	if err := s.RevokeCredential(ctx, "n1", 200); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetCredential(ctx, "n1")
	if got.State != CredRevoked || got.RevokedAt != 200 {
		t.Errorf("revoke failed: %+v", got)
	}
}

func TestEnrollToken_OneTimeUse(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := protocol.NowMillis()
	if err := s.PutEnrollToken(ctx, EnrollToken{TokenHash: "h1", State: EnrollUnused, ExpiresAt: now + 10000, CreatedAt: now}); err != nil {
		t.Fatal(err)
	}
	ok, err := s.ConsumeEnrollToken(ctx, "h1", now)
	if err != nil || !ok {
		t.Fatalf("first consume: ok=%v err=%v", ok, err)
	}
	ok, _ = s.ConsumeEnrollToken(ctx, "h1", now)
	if ok {
		t.Error("second consume should fail (one-time)")
	}
	// expired token
	_ = s.PutEnrollToken(ctx, EnrollToken{TokenHash: "h2", State: EnrollUnused, ExpiresAt: now - 1, CreatedAt: now})
	if ok, _ := s.ConsumeEnrollToken(ctx, "h2", now); ok {
		t.Error("expired token should not consume")
	}
}

func TestTask_LifecycleAndOfflineQueue(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	now := protocol.NowMillis()
	task := Task{TaskID: "t1", NodeID: "n1", Kind: TaskShell, Command: "echo hi",
		State: TaskQueued, CreatedAt: now, Deadline: now + 60000}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatal(err)
	}
	// queued for offline node
	q, err := s.QueuedTasks(ctx, "n1")
	if err != nil || len(q) != 1 {
		t.Fatalf("QueuedTasks: %v len=%d", err, len(q))
	}
	// queued -> sent records sent_at
	if err := s.UpdateTaskState(ctx, "t1", TaskSent, now+5); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetTask(ctx, "t1")
	if got.State != TaskSent || got.SentAt != now+5 {
		t.Errorf("sent: %+v", got)
	}
	// complete
	done := Task{TaskID: "t1", State: TaskSucceeded, FinishedAt: now + 10, ExitCode: 0,
		Stdout: "hi\n", Truncated: false, DurationMS: 5}
	if err := s.CompleteTask(ctx, done); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetTask(ctx, "t1")
	if got.State != TaskSucceeded || got.Stdout != "hi\n" || got.DurationMS != 5 {
		t.Errorf("complete: %+v", got)
	}
	// no longer queued
	if q, _ := s.QueuedTasks(ctx, "n1"); len(q) != 0 {
		t.Errorf("expected empty queue, got %d", len(q))
	}
	// list filter
	if l, _ := s.ListTasks(ctx, "n1", TaskSucceeded, 10); len(l) != 1 {
		t.Errorf("ListTasks succeeded: got %d", len(l))
	}
}

func TestActivity_AppendList(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	for i := 0; i < 3; i++ {
		_ = s.AppendActivity(ctx, Activity{NodeID: "n1", Type: "event", DetailJSON: "{}", At: int64(100 + i)})
	}
	list, err := s.ListActivities(ctx, "n1", 10, 0)
	if err != nil || len(list) != 3 {
		t.Fatalf("ListActivities: %v len=%d", err, len(list))
	}
	// newest first
	if list[0].At != 102 {
		t.Errorf("order wrong: %+v", list[0])
	}
	// before filter
	if l, _ := s.ListActivities(ctx, "n1", 10, 101); len(l) != 1 {
		t.Errorf("before=101 expected 1 got %d", len(l))
	}
}

func TestAdmin_PutGet(t *testing.T) {
	s := newTestStore(t)
	ctx := context.Background()
	if err := s.PutAdmin(ctx, Admin{Username: "admin", PasswordHash: "h", CreatedAt: 1}); err != nil {
		t.Fatal(err)
	}
	a, err := s.GetAdmin(ctx, "admin")
	if err != nil || a.PasswordHash != "h" {
		t.Fatalf("got %+v err %v", a, err)
	}
	if _, err := s.GetAdmin(ctx, "nobody"); err != ErrNotFound {
		t.Errorf("got %v want ErrNotFound", err)
	}
}
