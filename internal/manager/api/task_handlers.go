package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

// defaultTaskTimeoutSec applies when a shell task omits timeout_sec.
const defaultTaskTimeoutSec = 60

// POST /api/v1/nodes/{node_id}/tasks
// body {kind:"shell", command, timeout_sec} | {kind:"predefined", name, args}
func (a *API) handleCreateTask(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("node_id")
	var req struct {
		Kind       string          `json:"kind"`
		Command    string          `json:"command"`
		Name       string          `json:"name"`
		Args       json.RawMessage `json:"args"`
		TimeoutSec int             `json:"timeout_sec"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.Kind != store.TaskShell && req.Kind != store.TaskPredefined {
		writeError(w, http.StatusBadRequest, "bad_request", "kind must be shell or predefined")
		return
	}
	if req.Kind == store.TaskShell && req.Command == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "command required for shell task")
		return
	}
	if req.Kind == store.TaskPredefined && req.Name == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "name required for predefined task")
		return
	}
	if _, err := a.deps.Store.GetNode(r.Context(), nodeID); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}

	timeout := req.TimeoutSec
	if timeout <= 0 {
		timeout = defaultTaskTimeoutSec
	}
	now := protocol.NowMillis()
	task := store.Task{
		TaskID:    protocol.NewID(),
		NodeID:    nodeID,
		Kind:      req.Kind,
		Command:   req.Command,
		Name:      req.Name,
		ArgsJSON:  string(req.Args),
		Deadline:  now + int64(timeout)*1000,
		CreatedAt: now,
	}
	created, err := a.deps.Dispatcher.CreateTask(r.Context(), task)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "create task failed")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]any{"task_id": created.TaskID, "state": created.State})
}

// GET /api/v1/nodes/{node_id}/tasks?state=&limit=
func (a *API) handleListTasks(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("node_id")
	state := r.URL.Query().Get("state")
	limit := queryInt(r, "limit", 50, 500)
	tasks, err := a.deps.Store.ListTasks(r.Context(), nodeID, state, limit)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list tasks failed")
		return
	}
	if tasks == nil {
		tasks = []store.Task{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"tasks": tasks})
}

// GET /api/v1/tasks/{task_id}
func (a *API) handleGetTask(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("task_id")
	t, err := a.deps.Store.GetTask(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "task not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "get task failed")
		return
	}
	writeJSON(w, http.StatusOK, t)
}
