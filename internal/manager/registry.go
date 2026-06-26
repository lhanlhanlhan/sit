package manager

import (
	"sync"

	"github.com/sit/sit/internal/protocol"
	"github.com/sit/sit/internal/transport"
)

// OfflineTimeoutMS is the no-frame window after which a node is offline
// (3 heartbeat periods, ADR-005 / 02-transport §4).
const OfflineTimeoutMS = 90_000

// liveSession holds a connection plus its latest liveness data.
type liveSession struct {
	conn          transport.Conn
	lastSeen      int64
	lastHeartbeat protocol.Notification
}

// Registry tracks online node sessions: conn, last_seen, latest heartbeat.
// Safe for concurrent use.
type Registry struct {
	mu       sync.RWMutex
	sessions map[string]*liveSession
}

// NewRegistry creates an empty registry.
func NewRegistry() *Registry {
	return &Registry{sessions: make(map[string]*liveSession)}
}

// Add binds a connection to a node id (called after handshake auth). Replaces
// and closes any existing session for the same node.
func (r *Registry) Add(nodeID string, conn transport.Conn) {
	r.mu.Lock()
	if old, ok := r.sessions[nodeID]; ok {
		old.conn.Close("replaced by new session")
	}
	r.sessions[nodeID] = &liveSession{conn: conn, lastSeen: protocol.NowMillis()}
	r.mu.Unlock()
}

// Remove drops a node's session without closing the conn (caller owns close).
func (r *Registry) Remove(nodeID string) {
	r.mu.Lock()
	delete(r.sessions, nodeID)
	r.mu.Unlock()
}

// Conn returns the live connection for a node and whether it is online.
func (r *Registry) Conn(nodeID string) (transport.Conn, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[nodeID]
	if !ok {
		return nil, false
	}
	return s.conn, true
}

// IsOnline reports whether a node currently has a live session.
func (r *Registry) IsOnline(nodeID string) bool {
	r.mu.RLock()
	_, ok := r.sessions[nodeID]
	r.mu.RUnlock()
	return ok
}

// Touch records that a frame was received from a node now (refreshes last_seen).
func (r *Registry) Touch(nodeID string, now int64) {
	r.mu.Lock()
	if s, ok := r.sessions[nodeID]; ok {
		s.lastSeen = now
	}
	r.mu.Unlock()
}

// SetHeartbeat stores the latest heartbeat metrics and refreshes last_seen.
func (r *Registry) SetHeartbeat(nodeID string, hb protocol.Notification, now int64) {
	r.mu.Lock()
	if s, ok := r.sessions[nodeID]; ok {
		s.lastHeartbeat = hb
		s.lastSeen = now
	}
	r.mu.Unlock()
}

// LastSeen returns the node's last_seen and whether it is tracked.
func (r *Registry) LastSeen(nodeID string) (int64, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[nodeID]
	if !ok {
		return 0, false
	}
	return s.lastSeen, true
}

// LastHeartbeat returns the node's latest heartbeat notification.
func (r *Registry) LastHeartbeat(nodeID string) (protocol.Notification, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	s, ok := r.sessions[nodeID]
	if !ok {
		return protocol.Notification{}, false
	}
	return s.lastHeartbeat, true
}

// ReapStale closes and removes sessions with no frame for > OfflineTimeoutMS.
// Returns the node ids that were reaped (for status persistence/activity).
func (r *Registry) ReapStale(now int64) []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var reaped []string
	for id, s := range r.sessions {
		if now-s.lastSeen > OfflineTimeoutMS {
			s.conn.Close("heartbeat timeout")
			delete(r.sessions, id)
			reaped = append(reaped, id)
		}
	}
	return reaped
}

// OnlineIDs returns the ids of all currently online nodes.
func (r *Registry) OnlineIDs() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	ids := make([]string, 0, len(r.sessions))
	for id := range r.sessions {
		ids = append(ids, id)
	}
	return ids
}
