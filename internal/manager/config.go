package manager

import (
	"bufio"
	"os"
	"strings"
)

// Config is the manager.yaml runtime configuration.
type Config struct {
	ListenWSS     string // WSS listen address, e.g. ":443"
	ListenAPI     string // REST + MCP listen address, e.g. ":8443"
	StorePath     string // SQLite file path
	TLSCertFile   string // server cert (PEM)
	TLSKeyFile    string // server key (PEM)
	AdminUser     string // seed admin username
	AdminPassword string // seed admin password (first boot only)
	MCPToken      string // MCP endpoint bearer token (empty disables MCP auth)
}

// DefaultConfig returns config with sensible defaults.
func DefaultConfig() Config {
	return Config{
		ListenWSS: ":443",
		ListenAPI: ":8443",
		StorePath: "/var/lib/sit/manager.db",
	}
}

// LoadConfig parses a minimal YAML subset (flat key: value) from path.
func LoadConfig(path string) (Config, error) {
	cfg := DefaultConfig()
	f, err := os.Open(path)
	if err != nil {
		return Config{}, err
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		val = strings.Trim(strings.TrimSpace(val), `"'`)
		switch key {
		case "listen_wss":
			cfg.ListenWSS = val
		case "listen_api":
			cfg.ListenAPI = val
		case "store_path":
			cfg.StorePath = val
		case "tls_cert_file":
			cfg.TLSCertFile = val
		case "tls_key_file":
			cfg.TLSKeyFile = val
		case "admin_user":
			cfg.AdminUser = val
		case "admin_password":
			cfg.AdminPassword = val
		case "mcp_token":
			cfg.MCPToken = val
		}
	}
	return cfg, sc.Err()
}
