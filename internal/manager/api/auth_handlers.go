package api

import (
	"errors"
	"net/http"
	"time"

	"github.com/sit/sit/internal/manager"
)

// adminTokenTTL bounds an admin session lifetime.
const adminTokenTTL = 12 * time.Hour

// POST /api/v1/auth/login  body {username,password} -> {token, expires_at}
func (a *API) handleLogin(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !decodeJSON(w, r, &req) {
		return
	}
	token, exp, err := a.deps.Auth.Login(r.Context(), req.Username, req.Password, adminTokenTTL)
	if err != nil {
		writeError(w, http.StatusUnauthorized, "bad_credentials", "invalid username or password")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"token": token, "expires_at": exp})
}

// POST /api/v1/auth/logout
func (a *API) handleLogout(w http.ResponseWriter, r *http.Request) {
	a.deps.Auth.Logout(bearerToken(r))
	w.WriteHeader(http.StatusNoContent)
}

// GET /api/v1/auth/me
func (a *API) handleMe(w http.ResponseWriter, r *http.Request) {
	user, _ := r.Context().Value(ctxAdminUser).(string)
	writeJSON(w, http.StatusOK, map[string]any{"username": user})
}

// mapAuthError maps known auth errors to HTTP status codes.
func mapAuthError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, manager.ErrTokenInvalid):
		writeError(w, http.StatusBadRequest, "token_invalid", "token invalid or expired")
	case errors.Is(err, manager.ErrRevoked):
		writeError(w, http.StatusForbidden, "revoked", "credential revoked")
	default:
		writeError(w, http.StatusInternalServerError, "internal", "internal error")
	}
}
