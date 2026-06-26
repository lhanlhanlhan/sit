package managerd

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/node"
)

// TestE2E_EnrollConnectDispatch is the full-link acceptance test:
//  1. boot a Manager (real store + handlers) over httptest listeners,
//  2. mint an enroll token via the admin REST API,
//  3. enroll a node through the public exchange endpoint,
//  4. start the node Agent so it connects over WSS,
//  5. POST a shell task and poll until it reports success.
func TestE2E_EnrollConnectDispatch(t *testing.T) {
	dir := t.TempDir()
	cfg := manager.Config{
		StorePath:     filepath.Join(dir, "e2e.db"),
		AdminUser:     "admin",
		AdminPassword: "s3cret",
	}
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go srv.reapLoop(ctx)

	apiTS := httptest.NewServer(srv.apiHandler())
	defer apiTS.Close()
	wssTS := httptest.NewServer(srv.wssHandler())
	defer wssTS.Close()

	// --- 1. admin login ---
	adminTok := login(t, apiTS.URL, "admin", "s3cret")

	// --- 2. mint enroll token ---
	var mint struct {
		EnrollToken string `json:"enroll_token"`
	}
	doJSON(t, http.MethodPost, apiTS.URL+"/api/v1/nodes/enroll", adminTok, nil, &mint)
	if mint.EnrollToken == "" {
		t.Fatal("empty enroll_token")
	}

	// --- 3. enroll exchange (public) ---
	var enr struct {
		NodeID string `json:"node_id"`
		Secret string `json:"secret"`
	}
	doJSON(t, http.MethodPost, apiTS.URL+"/api/v1/enroll/exchange", "",
		map[string]string{"enroll_token": mint.EnrollToken}, &enr)
	if enr.NodeID == "" || enr.Secret == "" {
		t.Fatalf("bad enroll result: %+v", enr)
	}

	// --- 4. start node Agent ---
	wsURL := strings.Replace(wssTS.URL, "http://", "ws://", 1) + "/sit/connect"
	nodeCfg := node.Config{Endpoints: []string{wsURL}, HeartbeatSec: 1}
	id := node.Identity{NodeID: enr.NodeID, Secret: enr.Secret}
	agent := node.NewAgent(nodeCfg, id, "e2e-test", nil)
	agentCtx, agentCancel := context.WithCancel(ctx)
	defer agentCancel()
	go func() { _ = agent.Run(agentCtx) }()

	// Wait until the node is registered online.
	waitFor(t, 5*time.Second, func() bool {
		_, ok := srv.reg.Conn(enr.NodeID)
		return ok
	}, "node never connected")

	// --- 5. dispatch a shell task and poll result ---
	var created struct {
		TaskID string `json:"task_id"`
		State  string `json:"state"`
	}
	doJSON(t, http.MethodPost, apiTS.URL+"/api/v1/nodes/"+enr.NodeID+"/tasks", adminTok,
		map[string]any{"kind": "shell", "command": "echo hello-e2e", "timeout_sec": 10}, &created)
	if created.TaskID == "" {
		t.Fatal("empty task_id")
	}

	var final struct {
		State    string `json:"state"`
		ExitCode int    `json:"exit_code"`
		Stdout   string `json:"stdout"`
	}
	waitFor(t, 8*time.Second, func() bool {
		doJSON(t, http.MethodGet, apiTS.URL+"/api/v1/tasks/"+created.TaskID, adminTok, nil, &final)
		return final.State == "succeeded" || final.State == "failed"
	}, "task never completed")

	if final.State != "succeeded" {
		t.Fatalf("task state=%s exit=%d", final.State, final.ExitCode)
	}
	if !strings.Contains(final.Stdout, "hello-e2e") {
		t.Fatalf("unexpected stdout: %q", final.Stdout)
	}
}

// --- helpers ---

func login(t *testing.T, base, user, pass string) string {
	t.Helper()
	var out struct {
		Token string `json:"token"`
	}
	doJSON(t, http.MethodPost, base+"/api/v1/auth/login", "",
		map[string]string{"username": user, "password": pass}, &out)
	if out.Token == "" {
		t.Fatal("login returned empty token")
	}
	return out.Token
}

// doJSON performs an HTTP request with optional bearer + JSON body and decodes
// the JSON response into out (if non-nil). Fails the test on transport errors
// or non-2xx status.
func doJSON(t *testing.T, method, url, bearer string, body, out any) {
	t.Helper()
	var rdr io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		rdr = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, url, rdr)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if bearer != "" {
		req.Header.Set("Authorization", "Bearer "+bearer)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("%s %s: %v", method, url, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		b, _ := io.ReadAll(resp.Body)
		t.Fatalf("%s %s: status %d: %s", method, url, resp.StatusCode, strings.TrimSpace(string(b)))
	}
	if out != nil {
		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			t.Fatalf("decode %s: %v", url, err)
		}
	}
}

// waitFor polls cond every 50ms until true or timeout (then fails with msg).
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(msg)
}
