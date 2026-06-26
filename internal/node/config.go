package node

import (
	"bufio"
	"errors"
	"os"
	"strconv"
	"strings"
)

// Config is the node.yaml runtime configuration. Endpoints are NOT hardcoded so
// they can be updated out-of-band (ADR-004).
type Config struct {
	// Endpoints is an ordered list of wss:// Manager URLs (failover order).
	Endpoints []string
	// StateDir holds identity.json + credential (0600). Platform default if empty.
	StateDir string
	// HeartbeatSec is the heartbeat period (default 30).
	HeartbeatSec int
	// InsecureSkipVerify disables Manager TLS validation (TEST ONLY; never prod).
	InsecureSkipVerify bool
}

// ErrNoEndpoints indicates a config with no Manager endpoints.
var ErrNoEndpoints = errors.New("node: config has no endpoints")

// defaultHeartbeatSec matches 02-transport §4.
const defaultHeartbeatSec = 30

// LoadConfig parses a minimal YAML subset from path. Supported shape:
//
//	endpoints:
//	  - wss://a.example/sit/connect
//	  - wss://b.example/sit/connect
//	state_dir: /var/lib/sit
//	heartbeat_sec: 30
//	insecure_skip_verify: false
func LoadConfig(path string) (Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()
	return parseConfig(f)
}

func parseConfig(r interface{ Read([]byte) (int, error) }) (Config, error) {
	cfg := Config{HeartbeatSec: defaultHeartbeatSec}
	sc := bufio.NewScanner(r)
	inEndpoints := false
	for sc.Scan() {
		raw := sc.Text()
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		// List item under endpoints:
		if inEndpoints && strings.HasPrefix(line, "- ") {
			ep := strings.TrimSpace(strings.TrimPrefix(line, "- "))
			ep = strings.Trim(ep, `"'`)
			if ep != "" {
				cfg.Endpoints = append(cfg.Endpoints, ep)
			}
			continue
		}
		inEndpoints = false

		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.TrimSpace(val)
		val = strings.Trim(val, `"'`)
		switch key {
		case "endpoints":
			inEndpoints = true
		case "state_dir":
			cfg.StateDir = val
		case "heartbeat_sec":
			if n, err := strconv.Atoi(val); err == nil && n > 0 {
				cfg.HeartbeatSec = n
			}
		case "insecure_skip_verify":
			cfg.InsecureSkipVerify = (val == "true")
		}
	}
	if err := sc.Err(); err != nil {
		return Config{}, err
	}
	return cfg, nil
}
