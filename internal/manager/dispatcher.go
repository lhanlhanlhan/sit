package manager

import (
	"context"
	"encoding/json"
	"sync"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

// dedupWindow bounds the recent-result id set used for idempotent drops.
const dedupWindow = 4096

// Dispatcher creates tasks, sends them to online nodes (or queues them while
// offline), pairs results back, and enforces dedup/deadline/timeout.
type Dispatcher struct {
	store    store.Store
	registry *Registry

	mu        sync.Mutex
	seen      map[string]struct{} // recently processed result ref_ids
	seenOrder []string

	// waiters lets MCP/synchronous callers block until a task completes.
	waiters map[string]chan store.Task
}

// NewDispatcher constructs a Dispatcher.
func NewDispatcher(s store.Store, r *Registry) *Dispatcher {
	return &Dispatcher{
		store:    s,
		registry: r,
		seen:     make(map[string]struct{}),
		waiters:  make(map[string]chan store.Task),
	}
}

// CreateTask persists a new task (state=queued) and attempts immediate delivery
// if the node is online. Returns the created task.
func (d *Dispatcher) CreateTask(ctx context.Context, t store.Task) (store.Task, error) {
	t.State = store.TaskQueued
	if t.CreatedAt == 0 {
		t.CreatedAt = protocol.NowMillis()
	}
	if err := d.store.CreateTask(ctx, t); err != nil {
		return store.Task{}, err
	}
	d.appendActivity(ctx, t.NodeID, "task_sent", map[string]any{"task_id": t.TaskID, "kind": t.Kind})
	if d.registry.IsOnline(t.NodeID) {
		if err := d.send(ctx, t); err != nil {
			return t, nil // stays queued; will be delivered on reconnect
		}
		t.State = store.TaskSent
	}
	return t, nil
}

// send builds an instruction envelope from a task and writes it to the node's conn.
func (d *Dispatcher) send(ctx context.Context, t store.Task) error {
	conn, ok := d.registry.Conn(t.NodeID)
	if !ok {
		return store.ErrNotFound
	}
	instr := protocol.Instruction{
		Kind:       t.Kind,
		Command:    t.Command,
		Name:       t.Name,
		Deadline:   t.Deadline,
	}
	if t.ArgsJSON != "" {
		_ = json.Unmarshal([]byte(t.ArgsJSON), &instr.Args)
	}
	env, err := protocol.NewInstruction(instr)
	if err != nil {
		return err
	}
	env.ID = t.TaskID // task_id == instruction.id (idempotent re-send reuses id)
	if err := conn.Send(ctx, env); err != nil {
		return err
	}
	return d.store.UpdateTaskState(ctx, t.TaskID, store.TaskSent, protocol.NowMillis())
}

// FlushQueue delivers all queued tasks for a node in created_at order, dropping
// any whose deadline has passed (marked expired). Call on (re)connect.
func (d *Dispatcher) FlushQueue(ctx context.Context, nodeID string) error {
	tasks, err := d.store.QueuedTasks(ctx, nodeID)
	if err != nil {
		return err
	}
	now := protocol.NowMillis()
	for _, t := range tasks {
		if t.Deadline > 0 && now > t.Deadline {
			_ = d.store.UpdateTaskState(ctx, t.TaskID, store.TaskExpired, now)
			continue
		}
		_ = d.send(ctx, t)
	}
	return nil
}

// HandleResult pairs a result notification back to its task, applying dedup.
func (d *Dispatcher) HandleResult(ctx context.Context, n protocol.Notification) error {
	if n.RefID == "" {
		return nil
	}
	if d.seenRecently(n.RefID) {
		return nil // idempotent drop of duplicate result
	}
	d.markSeen(n.RefID)

	state := store.TaskSucceeded
	if n.TimedOut {
		state = store.TaskTimeout
	} else if n.ExitCode != 0 {
		state = store.TaskFailed
	}
	done := store.Task{
		TaskID:     n.RefID,
		State:      state,
		FinishedAt: protocol.NowMillis(),
		ExitCode:   n.ExitCode,
		Stdout:     n.Stdout,
		Stderr:     n.Stderr,
		Truncated:  n.Truncated,
		DurationMS: n.DurationMS,
	}
	if err := d.store.CompleteTask(ctx, done); err != nil {
		return err
	}
	// fetch the full row to hand to any synchronous waiter
	full, err := d.store.GetTask(ctx, n.RefID)
	if err == nil {
		d.appendActivity(ctx, full.NodeID, "task_result", map[string]any{"task_id": full.TaskID, "state": full.State})
		d.notifyWaiter(n.RefID, full)
	}
	return nil
}

// Wait registers a synchronous waiter for a task's completion (used by MCP).
// Returns a channel that receives the completed task exactly once.
func (d *Dispatcher) Wait(taskID string) <-chan store.Task {
	ch := make(chan store.Task, 1)
	d.mu.Lock()
	d.waiters[taskID] = ch
	d.mu.Unlock()
	return ch
}

// CancelWait removes a waiter (e.g. on timeout) to avoid leaks.
func (d *Dispatcher) CancelWait(taskID string) {
	d.mu.Lock()
	delete(d.waiters, taskID)
	d.mu.Unlock()
}

func (d *Dispatcher) notifyWaiter(taskID string, t store.Task) {
	d.mu.Lock()
	ch, ok := d.waiters[taskID]
	if ok {
		delete(d.waiters, taskID)
	}
	d.mu.Unlock()
	if ok {
		ch <- t
	}
}

func (d *Dispatcher) seenRecently(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	_, ok := d.seen[id]
	return ok
}

func (d *Dispatcher) markSeen(id string) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.seen[id] = struct{}{}
	d.seenOrder = append(d.seenOrder, id)
	if len(d.seenOrder) > dedupWindow {
		old := d.seenOrder[0]
		d.seenOrder = d.seenOrder[1:]
		delete(d.seen, old)
	}
}

func (d *Dispatcher) appendActivity(ctx context.Context, nodeID, typ string, detail map[string]any) {
	b, _ := json.Marshal(detail)
	_ = d.store.AppendActivity(ctx, store.Activity{
		NodeID:     nodeID,
		Type:       typ,
		DetailJSON: string(b),
		At:         protocol.NowMillis(),
	})
}
