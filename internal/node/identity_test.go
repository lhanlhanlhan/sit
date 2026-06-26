package node

import (
	"os"
	"path/filepath"
	"testing"
)

func TestIdentity_StableAcrossRestarts(t *testing.T) {
	dir := t.TempDir()
	id1, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("first load: %v", err)
	}
	if id1.NodeID == "" {
		t.Fatal("node_id should be generated on first run")
	}
	// Second load must return the SAME node_id.
	id2, err := LoadOrCreateIdentity(dir)
	if err != nil {
		t.Fatalf("second load: %v", err)
	}
	if id2.NodeID != id1.NodeID {
		t.Errorf("node_id not stable: %q != %q", id2.NodeID, id1.NodeID)
	}
}

func TestIdentity_FilePerms0600(t *testing.T) {
	dir := t.TempDir()
	id, _ := LoadOrCreateIdentity(dir)
	id.Secret = "super-secret"
	if err := SaveIdentity(dir, id); err != nil {
		t.Fatalf("save: %v", err)
	}
	info, err := os.Stat(filepath.Join(dir, identityFile))
	if err != nil {
		t.Fatalf("stat: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("identity file perms: got %o want 600", perm)
	}
	// Secret round-trips.
	got, _ := LoadOrCreateIdentity(dir)
	if got.Secret != "super-secret" {
		t.Errorf("secret not persisted: %q", got.Secret)
	}
}
