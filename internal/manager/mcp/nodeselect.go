// Package mcp implements the embedded MCP gateway: the Manager pretends to be a
// per-Node MCP Server (Streamable HTTP), selecting the target Node from a header
// or query param and routing tool calls over the existing Task/Instruction path.
// Node never implements MCP (docs/design/09-mcp.md).
package mcp

import (
	"net/http"
	"strings"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/store"
)

// nodeHeader / nodeParam select the target node (each Node = one MCP Server).
const (
	nodeHeader = "X-SIT-Node"
	nodeParam  = "node"
)

// resolveNode extracts the target node id from header (preferred) or query param.
func resolveNode(r *http.Request) string {
	if h := strings.TrimSpace(r.Header.Get(nodeHeader)); h != "" {
		return h
	}
	return strings.TrimSpace(r.URL.Query().Get(nodeParam))
}

// nodeGate verifies the node exists and has mcp_enabled. Returns the node and an
// HTTP status (0 = ok). Per §5 the blast radius is confined to opted-in nodes.
func nodeGate(s store.Store, r *http.Request) (store.Node, int) {
	id := resolveNode(r)
	if id == "" {
		return store.Node{}, http.StatusBadRequest
	}
	n, err := s.GetNode(r.Context(), id)
	if err != nil {
		return store.Node{}, http.StatusNotFound
	}
	if !n.MCPEnabled {
		return store.Node{}, http.StatusForbidden // mcp disabled for this node
	}
	return n, 0
}

// bearerToken extracts a bearer token from the Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// liveStatus overrides a stored node's status with live registry truth.
func liveStatus(reg *manager.Registry, n store.Node) string {
	if reg.IsOnline(n.NodeID) {
		return "online"
	}
	return n.Status
}
