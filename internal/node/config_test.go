package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfig_ParsesEndpointsAndDefaults(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.yaml")
	content := `# sample node config
endpoints:
  - wss://a.example/sit/connect
  - "wss://b.example/sit/connect"
state_dir: /var/lib/sit
heartbeat_sec: 45
insecure_skip_verify: true
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if len(cfg.Endpoints) != 2 || cfg.Endpoints[0] != "wss://a.example/sit/connect" || cfg.Endpoints[1] != "wss://b.example/sit/connect" {
		t.Errorf("endpoints: %+v", cfg.Endpoints)
	}
	if cfg.StateDir != "/var/lib/sit" {
		t.Errorf("state_dir: %q", cfg.StateDir)
	}
	if cfg.HeartbeatSec != 45 {
		t.Errorf("heartbeat_sec: %d", cfg.HeartbeatSec)
	}
	if !cfg.InsecureSkipVerify {
		t.Error("insecure_skip_verify should be true")
	}
}

func TestConfig_DefaultHeartbeat(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "node.yaml")
	_ = os.WriteFile(path, []byte("endpoints:\n  - wss://x/sit/connect\n"), 0o644)
	cfg, err := LoadConfig(path)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.HeartbeatSec != defaultHeartbeatSec {
		t.Errorf("default heartbeat: got %d want %d", cfg.HeartbeatSec, defaultHeartbeatSec)
	}
}
