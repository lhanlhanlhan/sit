package api

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
)

// testEnv wires a full API over a temp SQLite store plus a seeded admin.
type testEnv struct {
	srv   *httptest.Server
	deps  Deps
	token string
}

func newTestEnv(t *testing.T) *testEnv {
	t.Helper()
	s, err := store.OpenSQLite(filepath.Join(t.TempDir(), "api.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { s.Close() })

	auth := manager.NewAuth(s)
	if err := auth.SeedAdmin(context.Background(), "admin", "secret"); err != nil {
		t.Fatalf("seed admin: %v", err)
	}
	reg := manager.NewRegistry()
	disp := manager.NewDispatcher(s, reg)
	deps := Deps{Auth: auth, Store: s, Registry: reg, Dispatcher: disp}
	srv := httptest.NewServer(New(deps).Handler())
	t.Cleanup(srv.Close)

	e := &testEnv{srv: srv, deps: deps}
	e.token = e.login(t, "admin", "secret")
	return e
}

func (e *testEnv) do(t *testing.T, method, path string, body any, auth bool) *http.Response {
	t.Helper()
	var rdr *bytes.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	} else {
		rdr = bytes.NewReader(nil)
	}
	req, err := http.NewRequest(method, e.srv.URL+path, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if auth {
		req.Header.Set("Authorization", "Bearer "+e.token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do %s %s: %v", method, path, err)
	}
	return resp
}

func decode(t *testing.T, resp *http.Response, v any) {
	t.Helper()
	defer resp.Body.Close()
	if v != nil {
		if err := json.NewDecoder(resp.Body).Decode(v); err != nil {
			t.Fatalf("decode: %v", err)
		}
	}
}

func (e *testEnv) login(t *testing.T, user, pass string) string {
	t.Helper()
	resp := e.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": user, "password": pass}, false)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("login status: %d", resp.StatusCode)
	}
	var out struct {
		Token     string `json:"token"`
		ExpiresAt int64  `json:"expires_at"`
	}
	decode(t, resp, &out)
	if out.Token == "" || out.ExpiresAt == 0 {
		t.Fatalf("login response missing token/expiry: %+v", out)
	}
	return out.Token
}

func TestAPI_LoginAndProtectedRoutes(t *testing.T) {
	e := newTestEnv(t)

	// Wrong password rejected.
	resp := e.do(t, "POST", "/api/v1/auth/login", map[string]string{"username": "admin", "password": "wrong"}, false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("bad login: got %d want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// No token -> 401.
	resp = e.do(t, "GET", "/api/v1/nodes", nil, false)
	if resp.StatusCode != http.StatusUnauthorized {
		t.Errorf("no token: got %d want 401", resp.StatusCode)
	}
	resp.Body.Close()

	// With token -> 200 and /me reflects the admin.
	resp = e.do(t, "GET", "/api/v1/auth/me", nil, true)
	var me struct {
		Username string `json:"username"`
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("me status: %d", resp.StatusCode)
	}
	decode(t, resp, &me)
	if me.Username != "admin" {
		t.Errorf("me username: %q", me.Username)
	}
}

func TestAPI_NodeListDetailRenameDelete(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	now := protocol.NowMillis()
	addrs, _ := json.Marshal([]protocol.Addr{{IP: "10.0.0.1", Family: "v4", Iface: "eth0", Scope: "private"}})
	_ = e.deps.Store.UpsertNode(ctx, store.Node{
		NodeID: "n1", DisplayName: "box", OS: "linux", Arch: "arm64", Version: "sit/0.1.0",
		AddrsJSON: string(addrs), Status: "offline", LastSeen: now, CreatedAt: now,
	})

	// List.
	resp := e.do(t, "GET", "/api/v1/nodes", nil, true)
	var list struct {
		Nodes []nodeDTO `json:"nodes"`
	}
	decode(t, resp, &list)
	if len(list.Nodes) != 1 || list.Nodes[0].NodeID != "n1" {
		t.Fatalf("list nodes: %+v", list.Nodes)
	}
	if len(list.Nodes[0].Addrs) != 1 || list.Nodes[0].Addrs[0].IP != "10.0.0.1" {
		t.Errorf("addrs not decoded: %+v", list.Nodes[0].Addrs)
	}

	// Detail.
	resp = e.do(t, "GET", "/api/v1/nodes/n1", nil, true)
	var det nodeDTO
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("detail status: %d", resp.StatusCode)
	}
	decode(t, resp, &det)
	if det.OS != "linux" {
		t.Errorf("detail os: %q", det.OS)
	}

	// Rename.
	resp = e.do(t, "PATCH", "/api/v1/nodes/n1", map[string]string{"display_name": "renamed"}, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("rename status: %d", resp.StatusCode)
	}
	resp.Body.Close()
	got, _ := e.deps.Store.GetNode(ctx, "n1")
	if got.DisplayName != "renamed" {
		t.Errorf("display name: %q", got.DisplayName)
	}

	// Detail of missing node -> 404.
	resp = e.do(t, "GET", "/api/v1/nodes/ghost", nil, true)
	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("ghost detail: got %d want 404", resp.StatusCode)
	}
	resp.Body.Close()

	// Delete.
	resp = e.do(t, "DELETE", "/api/v1/nodes/n1", nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("delete status: %d", resp.StatusCode)
	}
	resp.Body.Close()
	if _, err := e.deps.Store.GetNode(ctx, "n1"); err != store.ErrNotFound {
		t.Errorf("node not deleted: %v", err)
	}
}

func TestAPI_MCPEnableDisable(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	_ = e.deps.Store.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]"})

	resp := e.do(t, "POST", "/api/v1/nodes/n1/mcp:enable", nil, true)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("enable status: %d", resp.StatusCode)
	}
	resp.Body.Close()
	got, _ := e.deps.Store.GetNode(ctx, "n1")
	if !got.MCPEnabled {
		t.Error("mcp should be enabled")
	}

	resp = e.do(t, "POST", "/api/v1/nodes/n1/mcp:disable", nil, true)
	resp.Body.Close()
	got, _ = e.deps.Store.GetNode(ctx, "n1")
	if got.MCPEnabled {
		t.Error("mcp should be disabled")
	}
}

func TestAPI_CreateTaskReturnsIDAndPolls(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	_ = e.deps.Store.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]"})

	// Node offline -> task queued.
	resp := e.do(t, "POST", "/api/v1/nodes/n1/tasks", map[string]any{"kind": "shell", "command": "echo hi", "timeout_sec": 30}, true)
	if resp.StatusCode != http.StatusAccepted {
		t.Fatalf("create task status: %d", resp.StatusCode)
	}
	var created struct {
		TaskID string `json:"task_id"`
		State  string `json:"state"`
	}
	decode(t, resp, &created)
	if created.TaskID == "" {
		t.Fatal("no task_id returned")
	}
	if created.State != store.TaskQueued {
		t.Errorf("offline task state: got %q want queued", created.State)
	}

	// Simulate a result coming back.
	_ = e.deps.Dispatcher.HandleResult(ctx, protocol.Notification{
		Kind: protocol.KindResult, RefID: created.TaskID, ExitCode: 0, Stdout: "hi\n",
	})

	// Poll terminal state.
	resp = e.do(t, "GET", "/api/v1/tasks/"+created.TaskID, nil, true)
	var task store.Task
	decode(t, resp, &task)
	if task.State != store.TaskSucceeded || task.Stdout != "hi\n" {
		t.Errorf("polled task: %+v", task)
	}

	// Bad kind rejected.
	resp = e.do(t, "POST", "/api/v1/nodes/n1/tasks", map[string]any{"kind": "bogus"}, true)
	if resp.StatusCode != http.StatusBadRequest {
		t.Errorf("bad kind: got %d want 400", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestAPI_EnrollAndRevoke(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()

	// Enroll mints a one-time token.
	resp := e.do(t, "POST", "/api/v1/nodes/enroll", nil, true)
	var en struct {
		EnrollToken string `json:"enroll_token"`
		ExpiresAt   int64  `json:"expires_at"`
	}
	decode(t, resp, &en)
	if en.EnrollToken == "" {
		t.Fatal("no enroll token")
	}

	// Use it to create a credential + node (via Auth directly, mirroring node enroll).
	secret, nodeID, err := e.deps.Auth.Enroll(ctx, en.EnrollToken, "")
	if err != nil || secret == "" || nodeID == "" {
		t.Fatalf("enroll: secret=%q node=%q err=%v", secret, nodeID, err)
	}
	if err := e.deps.Auth.VerifyNode(ctx, nodeID, secret); err != nil {
		t.Fatalf("verify before revoke: %v", err)
	}

	// Revoke via API.
	resp = e.do(t, "POST", "/api/v1/nodes/"+nodeID+"/revoke", nil, true)
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("revoke status: %d", resp.StatusCode)
	}
	resp.Body.Close()
	if err := e.deps.Auth.VerifyNode(ctx, nodeID, secret); err != manager.ErrRevoked {
		t.Errorf("verify after revoke: got %v want ErrRevoked", err)
	}
}

func TestAPI_Activities(t *testing.T) {
	e := newTestEnv(t)
	ctx := context.Background()
	_ = e.deps.Store.UpsertNode(ctx, store.Node{NodeID: "n1", AddrsJSON: "[]"})
	_ = e.deps.Store.AppendActivity(ctx, store.Activity{NodeID: "n1", Type: "register", DetailJSON: "{}", At: time.Now().UnixMilli()})

	resp := e.do(t, "GET", "/api/v1/nodes/n1/activities", nil, true)
	var out struct {
		Activities []store.Activity `json:"activities"`
	}
	decode(t, resp, &out)
	if len(out.Activities) != 1 || out.Activities[0].Type != "register" {
		t.Errorf("activities: %+v", out.Activities)
	}
}
