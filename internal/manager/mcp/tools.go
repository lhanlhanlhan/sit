package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

// mcpCallTimeout bounds a synchronous run_command wait. On overrun the caller
// receives the task_id for later polling (graceful degradation, §4).
const mcpCallTimeout = 120 * time.Second

// toolDef is an MCP tool advertisement.
type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// toolDefs lists the v1 tools (no list_nodes — node selection is routing, §3).
func toolDefs() []toolDef {
	return []toolDef{
		{
			Name:        "run_command",
			Description: "Synchronously run a non-interactive shell command on the selected Node and return stdout/stderr/exit_code.",
			InputSchema: map[string]any{
				"type": "object",
				"properties": map[string]any{
					"command":     map[string]any{"type": "string", "description": "non-interactive shell command"},
					"timeout_sec": map[string]any{"type": "integer", "description": "execution timeout, default 60"},
				},
				"required": []string{"command"},
			},
		},
		{
			Name:        "get_status",
			Description: "Return the selected Node's status, metrics, and last_seen.",
			InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		},
	}
}

// runCommand creates a Task, dispatches it over WSS, and blocks for the result
// up to mcpCallTimeout. Reuses the existing Task/dispatcher path (§4).
func runCommand(ctx context.Context, deps Deps, nodeID string, args map[string]any) (map[string]any, error) {
	command, _ := args["command"].(string)
	timeoutSec := 60
	if v, ok := args["timeout_sec"].(float64); ok && v > 0 {
		timeoutSec = int(v)
	}
	now := protocol.NowMillis()
	taskID := protocol.NewID()
	task := store.Task{
		TaskID:    taskID,
		NodeID:    nodeID,
		Kind:      store.TaskShell,
		Command:   command,
		Deadline:  now + int64(timeoutSec)*1000,
		CreatedAt: now,
	}
	// Register the waiter BEFORE dispatch to avoid a result race.
	ch := deps.Dispatcher.Wait(taskID)
	if _, err := deps.Dispatcher.CreateTask(ctx, task); err != nil {
		deps.Dispatcher.CancelWait(taskID)
		return nil, err
	}

	select {
	case <-ctx.Done():
		deps.Dispatcher.CancelWait(taskID)
		return nil, ctx.Err()
	case <-time.After(mcpCallTimeout):
		// Degrade: return task_id so the agent can poll later.
		deps.Dispatcher.CancelWait(taskID)
		return map[string]any{"task_id": taskID, "pending": true}, nil
	case done := <-ch:
		return map[string]any{
			"stdout":      done.Stdout,
			"stderr":      done.Stderr,
			"exit_code":   done.ExitCode,
			"duration_ms": done.DurationMS,
			"truncated":   done.Truncated,
			"timed_out":   done.State == store.TaskTimeout,
			"state":       done.State,
		}, nil
	}
}

// getStatus returns the node's status/metrics snapshot.
func getStatus(deps Deps, reg *manager.Registry, n store.Node) map[string]any {
	var addrs []protocol.Addr
	if n.AddrsJSON != "" {
		_ = json.Unmarshal([]byte(n.AddrsJSON), &addrs)
	}
	out := map[string]any{
		"node_id":      n.NodeID,
		"display_name": n.DisplayName,
		"status":       liveStatus(reg, n),
		"last_seen":    n.LastSeen,
		"os":           n.OS,
		"arch":         n.Arch,
		"version":      n.Version,
		"addrs":        addrs,
	}
	if hb, ok := reg.LastHeartbeat(n.NodeID); ok && hb.Kind == protocol.KindHeartbeat {
		out["last_heartbeat"] = map[string]any{
			"uptime_sec":  hb.UptimeSec,
			"load":        hb.Load,
			"mem_used_mb": hb.MemUsedMB,
		}
	}
	return out
}
