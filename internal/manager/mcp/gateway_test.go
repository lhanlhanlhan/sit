package mcp

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/store"
	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// --- in-memory conn so CreateTask actually "sends" to an online node ---

type fakeConn struct {
	sent   chan protocol.Envelope
	doneCh chan struct{}
}

func newFakeConn() *fakeConn {
	return &fakeConn{sent: make(chan protocol.Envelope, 16), doneCh: make(chan struct{})}
}
func (f *fakeConn) Send(ctx context.Context, e protocol.Envelope) error { f.sent <- e; return nil }
func (f *fakeConn) Recv() <-chan protocol.Envelope                      { return nil }
func (f *fakeConn) Close(string) error                                  { return nil }
func (f *fakeConn) Info() transport.SessionInfo                         { return transport.SessionInfo{} }
func (f *fakeConn) Done() <-chan struct{}                               { return f.doneCh }

type env struct {
	srv   *httptest.Server
	deps  Deps
	reg   *manager.Registry
	store store.Store
}

func newEnv(t *testing.T, token string) *env {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "mcp.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })
	reg := manager.NewRegistry()
	disp := manager.NewDispatcher(s, reg)
	deps := Deps{Store: s, Registry: reg, Dispatcher: disp, MCPToken: token}
	srv := httptest.NewServer(New(deps).Handler())
	t.Cleanup(srv.Close)
	return &env{srv: srv, deps: deps, reg: reg, store: s}
}

func (e *env) rpc(t *testing.T, node, token string, body map[string]any) (*http.Response, map[string]any) {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if node != "" {
		req.Header.Set("X-SIT-Node", node)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rpc: %v", err)
	}
	var out map[string]any
	_ = json.NewDecoder(resp.Body).Decode(&out)
	resp.Body.Close()
	return resp, out
}

func (e *env) rpcRaw(t *testing.T, node, token string, body map[string]any) *http.Response {
	t.Helper()
	b, _ := json.Marshal(body)
	req, _ := http.NewRequest("POST", e.srv.URL+"/mcp", bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	if node != "" {
		req.Header.Set("X-SIT-Node", node)
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("rpc: %v", err)
	}
	t.Cleanup(func() { resp.Body.Close() })
	return resp
}

func TestMCP_BadTokenRejected(t *testing.T) {
	e := newEnv(t, "secret-mcp")
	resp, _ := e.rpc(t, "", "wrong", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad token: got %d want 401", resp.StatusCode)
	}
}

func TestMCP_ToolsList(t *testing.T) {
	e := newEnv(t, "")
	_, out := e.rpc(t, "", "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "tools/list"})
	res, _ := out["result"].(map[string]any)
	tools, _ := res["tools"].([]any)
	if len(tools) != 2 {
		t.Fatalf("expected 2 tools, got %d (%v)", len(tools), out)
	}
}

func TestMCP_InitializeUsesStreamableHTTPProtocolVersion(t *testing.T) {
	e := newEnv(t, "")
	_, out := e.rpc(t, "", "", map[string]any{"jsonrpc": "2.0", "id": 1, "method": "initialize"})
	res, _ := out["result"].(map[string]any)
	if res["protocolVersion"] != "2025-03-26" {
		t.Fatalf("protocolVersion: got %v want 2025-03-26", res["protocolVersion"])
	}
}

func TestMCP_InitializedNotificationAccepted(t *testing.T) {
	e := newEnv(t, "")
	resp := e.rpcRaw(t, "", "", map[string]any{"jsonrpc": "2.0", "method": "notifications/initialized"})
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("initialized notification: got status %d want 202", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "" {
		t.Fatalf("initialized notification content-type: got %q want empty", ct)
	}
}

func TestMCP_DisabledNodeForbidden(t *testing.T) {
	e := newEnv(t, "")
	_ = e.store.UpsertNode(context.Background(), store.Node{NodeID: "n1", AddrsJSON: "[]", MCPEnabled: false})
	_, out := e.rpc(t, "n1", "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "get_status", "arguments": map[string]any{}},
	})
	if out["error"] == nil {
		t.Fatalf("disabled node should error: %v", out)
	}
}

func TestMCP_GetStatus(t *testing.T) {
	e := newEnv(t, "")
	_ = e.store.UpsertNode(context.Background(), store.Node{
		NodeID: "n1", DisplayName: "box", OS: "linux", Arch: "arm64", AddrsJSON: "[]",
		Status: "offline", MCPEnabled: true,
	})
	_, out := e.rpc(t, "n1", "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "get_status", "arguments": map[string]any{}},
	})
	res, _ := out["result"].(map[string]any)
	sc, _ := res["structuredContent"].(map[string]any)
	if sc["node_id"] != "n1" || sc["os"] != "linux" {
		t.Errorf("get_status payload: %v", sc)
	}
}

func TestMCP_RunCommand(t *testing.T) {
	e := newEnv(t, "")
	ctx := context.Background()
	_ = e.store.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]", MCPEnabled: true})
	conn := newFakeConn()
	e.reg.Add("n1", conn)

	// Drain the dispatched instruction and feed back a result, so run_command unblocks.
	go func() {
		select {
		case envl := <-conn.sent:
			_ = e.deps.Dispatcher.HandleResult(ctx, protocol.Notification{
				Kind: protocol.KindResult, RefID: envl.ID, ExitCode: 0, Stdout: "hello\n", DurationMS: 5,
			})
		case <-time.After(5 * time.Second):
		}
	}()

	_, out := e.rpc(t, "n1", "", map[string]any{
		"jsonrpc": "2.0", "id": 1, "method": "tools/call",
		"params": map[string]any{"name": "run_command", "arguments": map[string]any{"command": "echo hello", "timeout_sec": 30}},
	})
	res, _ := out["result"].(map[string]any)
	sc, _ := res["structuredContent"].(map[string]any)
	if sc["stdout"] != "hello\n" {
		t.Errorf("run_command stdout: %v (full %v)", sc["stdout"], out)
	}
	if code, _ := sc["exit_code"].(float64); code != 0 {
		t.Errorf("run_command exit_code: %v", sc["exit_code"])
	}
}
