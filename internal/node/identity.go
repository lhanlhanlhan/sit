// Package node implements the SIT resident agent: identity persistence, config
// loading, command execution, reporting, and the connect/recv loop.
package node

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/sit/sit/internal/protocol"
)

// Identity is a node's persistent local identity: a stable node_id (ULID) plus
// the long-term credential secret obtained at enrollment. Stored 0600.
type Identity struct {
	NodeID string `json:"node_id"`
	Secret string `json:"secret"`
}

// identityFile is the on-disk name within the state dir.
const identityFile = "identity.json"

// LoadOrCreateIdentity reads the identity from stateDir, generating a fresh
// node_id on first run. The secret is left empty until enrollment fills it.
// The file is always written with 0600 permissions.
func LoadOrCreateIdentity(stateDir string) (Identity, error) {
	path := filepath.Join(stateDir, identityFile)
	id, err := readIdentity(path)
	if err == nil {
		return id, nil
	}
	if !os.IsNotExist(err) {
		return Identity{}, err
	}
	// First run: mint a node_id, persist with empty secret.
	id = Identity{NodeID: protocol.NewID()}
	if err := writeIdentity(path, id); err != nil {
		return Identity{}, err
	}
	return id, nil
}

// SaveIdentity persists the identity (e.g. after enrollment fills the secret).
func SaveIdentity(stateDir string, id Identity) error {
	return writeIdentity(filepath.Join(stateDir, identityFile), id)
}

func readIdentity(path string) (Identity, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return Identity{}, err
	}
	return parseIdentity(b)
}

func writeIdentity(path string, id Identity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	b := encodeIdentity(id)
	// Write 0600: credential secret must not be world-readable.
	return os.WriteFile(path, b, 0o600)
}

// encodeIdentity/parseIdentity use a tiny line format to avoid pulling JSON
// indentation quirks; node_id and secret are single-line opaque tokens.
func encodeIdentity(id Identity) []byte {
	var sb strings.Builder
	sb.WriteString("node_id=")
	sb.WriteString(id.NodeID)
	sb.WriteString("\nsecret=")
	sb.WriteString(id.Secret)
	sb.WriteString("\n")
	return []byte(sb.String())
}

func parseIdentity(b []byte) (Identity, error) {
	var id Identity
	for _, line := range strings.Split(string(b), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		switch k {
		case "node_id":
			id.NodeID = v
		case "secret":
			id.Secret = v
		}
	}
	return id, nil
}
