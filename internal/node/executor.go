package node

import (
	"bytes"
	"context"
	"os/exec"
	"sync"
	"time"

	"github.com/sit/sit/internal/protocol"
)

// maxOutputBytes caps each of stdout/stderr; overflow sets Truncated (01-protocol §).
const maxOutputBytes = 64 << 10 // 64KB

// dedupWindow bounds the recently-executed instruction-id set for idempotency.
const dedupWindow = 1024

// PredefinedFunc is a whitelisted predefined command handler.
type PredefinedFunc func(ctx context.Context, args map[string]any) (stdout string, exitCode int, err error)

// Executor runs instructions: shell (arbitrary non-interactive) and predefined
// (whitelisted). It enforces timeout (process-group kill), output truncation,
// deadline checks, and per-instruction dedup.
type Executor struct {
	predefined map[string]PredefinedFunc

	mu        sync.Mutex
	seen      map[string]protocol.Notification // instruction id -> prior result
	seenOrder []string
}

// NewExecutor constructs an Executor with an optional predefined whitelist.
func NewExecutor(predefined map[string]PredefinedFunc) *Executor {
	if predefined == nil {
		predefined = map[string]PredefinedFunc{}
	}
	return &Executor{
		predefined: predefined,
		seen:       make(map[string]protocol.Notification),
	}
}

// Execute runs one instruction (identified by instrID for dedup) and returns the
// result notification (Kind=result, RefID=instrID). Idempotent: a repeated id
// replays the prior result.
func (e *Executor) Execute(ctx context.Context, instrID string, instr protocol.Instruction) protocol.Notification {
	if prior, ok := e.replay(instrID); ok {
		return prior
	}

	// Deadline check (unix ms). Past deadline => do not execute.
	if instr.Deadline > 0 && protocol.NowMillis() > instr.Deadline {
		res := protocol.Notification{Kind: protocol.KindResult, RefID: instrID, TimedOut: false, ExitCode: -1, Stderr: "deadline exceeded before execution"}
		e.remember(instrID, res)
		return res
	}

	var res protocol.Notification
	switch instr.Kind {
	case protocol.KindShell:
		res = e.runShell(ctx, instrID, instr)
	case protocol.KindPredefined:
		res = e.runPredefined(ctx, instrID, instr)
	default:
		res = protocol.Notification{Kind: protocol.KindResult, RefID: instrID, ExitCode: -1, Stderr: "unknown instruction kind"}
	}
	e.remember(instrID, res)
	return res
}

// runShell executes `sh -c <command>` with timeout-driven process-group kill.
func (e *Executor) runShell(ctx context.Context, instrID string, instr protocol.Instruction) protocol.Notification {
	timeout := time.Duration(instr.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	cmd := exec.CommandContext(runCtx, "sh", "-c", instr.Command)
	configureProcGroup(cmd) // platform-specific: own process group for clean kill
	cmd.Cancel = func() error { return killGroup(cmd) }

	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	cmd.Stdin = nil // non-interactive only

	start := time.Now()
	err := cmd.Run()
	dur := time.Since(start).Milliseconds()

	res := protocol.Notification{Kind: protocol.KindResult, RefID: instrID, DurationMS: dur}
	res.Stdout, res.Truncated = truncate(stdout.Bytes())
	se, t2 := truncate(stderr.Bytes())
	res.Stderr = se
	res.Truncated = res.Truncated || t2

	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
		res.ExitCode = -1
		return res
	}
	res.ExitCode = exitCode(err)
	return res
}

// runPredefined dispatches to a whitelisted handler by name.
func (e *Executor) runPredefined(ctx context.Context, instrID string, instr protocol.Instruction) protocol.Notification {
	fn, ok := e.predefined[instr.Name]
	if !ok {
		return protocol.Notification{Kind: protocol.KindResult, RefID: instrID, ExitCode: -1, Stderr: "unknown predefined command: " + instr.Name}
	}
	timeout := time.Duration(instr.TimeoutSec) * time.Second
	if timeout <= 0 {
		timeout = 60 * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	start := time.Now()
	out, code, err := fn(runCtx, instr.Args)
	dur := time.Since(start).Milliseconds()
	res := protocol.Notification{Kind: protocol.KindResult, RefID: instrID, DurationMS: dur, ExitCode: code}
	res.Stdout, res.Truncated = truncate([]byte(out))
	if err != nil {
		se, t2 := truncate([]byte(err.Error()))
		res.Stderr = se
		res.Truncated = res.Truncated || t2
		if code == 0 {
			res.ExitCode = -1
		}
	}
	if runCtx.Err() == context.DeadlineExceeded {
		res.TimedOut = true
	}
	return res
}

// exitCode extracts a process exit code from a Run() error.
func exitCode(err error) int {
	if err == nil {
		return 0
	}
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode()
	}
	return -1
}

// truncate clamps b to maxOutputBytes, reporting whether truncation occurred.
func truncate(b []byte) (string, bool) {
	if len(b) > maxOutputBytes {
		return string(b[:maxOutputBytes]), true
	}
	return string(b), false
}

func (e *Executor) replay(id string) (protocol.Notification, bool) {
	e.mu.Lock()
	defer e.mu.Unlock()
	n, ok := e.seen[id]
	return n, ok
}

func (e *Executor) remember(id string, n protocol.Notification) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if _, ok := e.seen[id]; ok {
		return
	}
	e.seen[id] = n
	e.seenOrder = append(e.seenOrder, id)
	if len(e.seenOrder) > dedupWindow {
		old := e.seenOrder[0]
		e.seenOrder = e.seenOrder[1:]
		delete(e.seen, old)
	}
}
