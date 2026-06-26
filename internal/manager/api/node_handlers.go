package api

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
)

// nodeDTO is the API representation of a node (addrs decoded, status authoritative).
type nodeDTO struct {
	NodeID      string           `json:"node_id"`
	DisplayName string           `json:"display_name"`
	Status      string           `json:"status"`
	LastSeen    int64            `json:"last_seen"`
	OS          string           `json:"os"`
	Arch        string           `json:"arch"`
	Version     string           `json:"version"`
	Hostname    string           `json:"hostname"`
	Addrs       []protocol.Addr  `json:"addrs"`
	MCPEnabled  bool             `json:"mcp_enabled"`
	CreatedAt   int64            `json:"created_at"`
	Heartbeat   *heartbeatMetric `json:"heartbeat,omitempty"`
}

// heartbeatMetric is the latest heartbeat snapshot for the detail view.
type heartbeatMetric struct {
	UptimeSec int64     `json:"uptime_sec"`
	Load      []float64 `json:"load"`
	MemUsedMB int64     `json:"mem_used_mb"`
}

// toDTO converts a stored node, overriding status with live registry truth.
func (a *API) toDTO(n store.Node) nodeDTO {
	var addrs []protocol.Addr
	if n.AddrsJSON != "" {
		_ = json.Unmarshal([]byte(n.AddrsJSON), &addrs)
	}
	status := n.Status
	if a.deps.Registry.IsOnline(n.NodeID) {
		status = "online"
	}
	return nodeDTO{
		NodeID: n.NodeID, DisplayName: n.DisplayName, Status: status,
		LastSeen: n.LastSeen, OS: n.OS, Arch: n.Arch, Version: n.Version,
		Hostname: n.Hostname, Addrs: addrs, MCPEnabled: n.MCPEnabled, CreatedAt: n.CreatedAt,
	}
}

// GET /api/v1/nodes?status=&q=
func (a *API) handleListNodes(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	nodes, err := a.deps.Store.ListNodes(r.Context(), q.Get("status"), q.Get("q"))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list nodes failed")
		return
	}
	out := make([]nodeDTO, 0, len(nodes))
	for _, n := range nodes {
		out = append(out, a.toDTO(n))
	}
	writeJSON(w, http.StatusOK, map[string]any{"nodes": out})
}

// GET /api/v1/nodes/{node_id}
func (a *API) handleGetNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("node_id")
	n, err := a.deps.Store.GetNode(r.Context(), id)
	if errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "get node failed")
		return
	}
	dto := a.toDTO(n)
	if hb, ok := a.deps.Registry.LastHeartbeat(id); ok && hb.Kind == protocol.KindHeartbeat {
		dto.Heartbeat = &heartbeatMetric{UptimeSec: hb.UptimeSec, Load: hb.Load, MemUsedMB: hb.MemUsedMB}
	}
	writeJSON(w, http.StatusOK, dto)
}

// PATCH /api/v1/nodes/{node_id}  body {display_name}
func (a *API) handlePatchNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("node_id")
	var req struct {
		DisplayName string `json:"display_name"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if _, err := a.deps.Store.GetNode(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	if err := a.deps.Store.SetDisplayName(r.Context(), id, req.DisplayName); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "rename failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// DELETE /api/v1/nodes/{node_id}  -- removes node and revokes its credential.
func (a *API) handleDeleteNode(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("node_id")
	_ = a.deps.Auth.RevokeNode(r.Context(), id) // best effort: kick + refuse reconnect
	a.deps.Registry.Remove(id)
	if err := a.deps.Store.DeleteNode(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "delete failed")
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/nodes/{node_id}/mcp:enable
func (a *API) handleMCPEnable(w http.ResponseWriter, r *http.Request) { a.setMCP(w, r, true) }

// POST /api/v1/nodes/{node_id}/mcp:disable
func (a *API) handleMCPDisable(w http.ResponseWriter, r *http.Request) { a.setMCP(w, r, false) }

func (a *API) setMCP(w http.ResponseWriter, r *http.Request, enabled bool) {
	id := r.PathValue("node_id")
	if _, err := a.deps.Store.GetNode(r.Context(), id); errors.Is(err, store.ErrNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "node not found")
		return
	}
	if err := a.deps.Store.SetMCPEnabled(r.Context(), id, enabled); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mcp toggle failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_id": id, "mcp_enabled": enabled})
}
