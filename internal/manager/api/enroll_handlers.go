package api

import (
	"net/http"
	"time"
)

// enrollTokenTTL bounds a one-time enrollment token lifetime.
const enrollTokenTTL = 30 * time.Minute

// POST /api/v1/nodes/enroll -> {enroll_token, expires_at}
func (a *API) handleEnroll(w http.ResponseWriter, r *http.Request) {
	token, exp, err := a.deps.Auth.MintEnrollToken(r.Context(), enrollTokenTTL)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "mint enroll token failed")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"enroll_token": token, "expires_at": exp})
}

// POST /api/v1/nodes/{node_id}/revoke -- kicks node + refuses reconnect.
func (a *API) handleRevoke(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("node_id")
	if err := a.deps.Auth.RevokeNode(r.Context(), id); err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "revoke failed")
		return
	}
	if conn, ok := a.deps.Registry.Conn(id); ok {
		conn.Close("credential revoked")
		a.deps.Registry.Remove(id)
	}
	w.WriteHeader(http.StatusNoContent)
}

// POST /api/v1/enroll/exchange  body {enroll_token, node_id?} -> {node_id, secret}
// Public endpoint: authenticated by the one-time enroll_token itself. The node
// presents its self-generated node_id (optional); Manager issues a long-term
// credential. The token is single-use and consumed atomically.
func (a *API) handleEnrollExchange(w http.ResponseWriter, r *http.Request) {
	var req struct {
		EnrollToken string `json:"enroll_token"`
		NodeID      string `json:"node_id"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.EnrollToken == "" {
		writeError(w, http.StatusBadRequest, "bad_request", "enroll_token required")
		return
	}
	secret, nodeID, err := a.deps.Auth.Enroll(r.Context(), req.EnrollToken, req.NodeID)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "token_invalid", "enroll token invalid or already used")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"node_id": nodeID, "secret": secret})
}
