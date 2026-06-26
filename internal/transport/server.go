package transport

import (
	"context"
	"net/http"
	"strings"

	"github.com/coder/websocket"
	"github.com/sit/sit/internal/protocol"
)

// AuthFunc validates the WS upgrade request and returns the authenticated
// node identity. The returned NodeID is authoritative and overrides any
// self-reported register payload. An error rejects the handshake (401).
type AuthFunc func(r *http.Request) (nodeID string, err error)

// ConnHandler receives an authenticated, pumped connection. It owns the
// connection lifecycle from here (read loop, close).
type ConnHandler func(Conn)

// Server upgrades inbound HTTP requests to authenticated WSS connections.
type Server struct {
	Auth    AuthFunc
	Handler ConnHandler
}

// ServeHTTP implements http.Handler for the WSS endpoint (e.g. /sit/connect).
// Auth runs on the raw HTTP request BEFORE the upgrade, so rejected nodes
// never establish a websocket.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	nodeID, err := s.Auth(r)
	if err != nil {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	ws, err := websocket.Accept(w, r, nil)
	if err != nil {
		return // Accept already wrote an error response
	}
	conn := newWSConn(ws, SessionInfo{
		NodeID:      nodeID,
		RemoteAddr:  r.RemoteAddr,
		ConnectedAt: protocol.NowMillis(),
	})
	s.Handler(conn)
}

// BearerToken extracts a bearer token from the Authorization header, or "".
func BearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if strings.HasPrefix(h, p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// compile-time guard
var _ = context.Background
