package mcp

import (
	"encoding/json"
	"net/http"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/store"
)

// Deps are the manager-core collaborators the gateway routes into.
type Deps struct {
	Store      store.Store
	Registry   *manager.Registry
	Dispatcher *manager.Dispatcher
	// MCPToken is the bearer token gating the MCP endpoint (separate from admin
	// and node credentials — three-credential isolation, §6). Empty disables auth
	// (test only).
	MCPToken string
}

// Gateway is the embedded MCP server (Streamable HTTP). It speaks a minimal
// JSON-RPC 2.0 subset: initialize, tools/list, tools/call.
type Gateway struct {
	deps Deps
}

// New constructs a Gateway.
func New(d Deps) *Gateway { return &Gateway{deps: d} }

// rpcRequest is a JSON-RPC 2.0 request.
type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params"`
}

// rpcResponse is a JSON-RPC 2.0 response.
type rpcResponse struct {
	JSONRPC string    `json:"jsonrpc"`
	ID      any       `json:"id"`
	Result  any       `json:"result,omitempty"`
	Error   *rpcError `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

// Handler returns the MCP HTTP handler mounted at /mcp.
func (g *Gateway) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/mcp", g.serveRPC)
	return mux
}

func (g *Gateway) serveRPC(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	// MCP bearer auth (separate credential system).
	if g.deps.MCPToken != "" && bearerToken(r) != g.deps.MCPToken {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}

	var req rpcRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeRPCError(w, nil, -32700, "parse error")
		return
	}

	switch req.Method {
	case "initialize":
		g.writeResult(w, req.ID, map[string]any{
			"protocolVersion": "2024-11-05",
			"serverInfo":      map[string]any{"name": "sit-manager", "version": "0.1.0"},
			"capabilities":    map[string]any{"tools": map[string]any{}},
		})
	case "tools/list":
		g.writeResult(w, req.ID, map[string]any{"tools": toolDefs()})
	case "tools/call":
		g.handleToolCall(w, r, req)
	default:
		writeRPCError(w, req.ID, -32601, "method not found: "+req.Method)
	}
}

// callParams is the tools/call params shape.
type callParams struct {
	Name      string         `json:"name"`
	Arguments map[string]any `json:"arguments"`
}

func (g *Gateway) handleToolCall(w http.ResponseWriter, r *http.Request, req rpcRequest) {
	var p callParams
	if err := json.Unmarshal(req.Params, &p); err != nil {
		writeRPCError(w, req.ID, -32602, "invalid params")
		return
	}
	// Node gate: existence + mcp_enabled (§5).
	node, status := nodeGate(g.deps.Store, r)
	if status != 0 {
		writeRPCError(w, req.ID, -32000, http.StatusText(status))
		return
	}

	switch p.Name {
	case "run_command":
		out, err := runCommand(r.Context(), g.deps, node.NodeID, p.Arguments)
		if err != nil {
			writeRPCError(w, req.ID, -32000, err.Error())
			return
		}
		g.writeToolResult(w, req.ID, out)
	case "get_status":
		g.writeToolResult(w, req.ID, getStatus(g.deps, g.deps.Registry, node))
	default:
		writeRPCError(w, req.ID, -32601, "unknown tool: "+p.Name)
	}
}

// writeToolResult wraps a structured payload in the MCP content envelope.
func (g *Gateway) writeToolResult(w http.ResponseWriter, id any, payload any) {
	b, _ := json.Marshal(payload)
	g.writeResult(w, id, map[string]any{
		"content":           []map[string]any{{"type": "text", "text": string(b)}},
		"structuredContent": payload,
		"isError":           false,
	})
}

func (g *Gateway) writeResult(w http.ResponseWriter, id any, result any) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Result: result})
}

func writeRPCError(w http.ResponseWriter, id any, code int, msg string) {
	writeJSON(w, http.StatusOK, rpcResponse{JSONRPC: "2.0", ID: normalizeID(id), Error: &rpcError{Code: code, Message: msg}})
}

// normalizeID unwraps a json.RawMessage id into a plain value for echoing.
func normalizeID(id any) any {
	if raw, ok := id.(json.RawMessage); ok {
		var v any
		if json.Unmarshal(raw, &v) == nil {
			return v
		}
		return nil
	}
	return id
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
