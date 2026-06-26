// Package api implements the Manager REST API (/api/v1) per docs/design/03-rest-api.md.
// It is a thin HTTP layer over the manager core (auth, store, registry, dispatcher);
// all business rules live below this package.
package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"github.com/sit/sit/internal/manager"
	"github.com/sit/sit/internal/manager/store"
)

// Deps are the manager-core collaborators the API handlers route into.
type Deps struct {
	Auth       *manager.Auth
	Store      store.Store
	Registry   *manager.Registry
	Dispatcher *manager.Dispatcher
}

// API holds dependencies and builds the HTTP handler.
type API struct {
	deps Deps
}

// New constructs an API over the given dependencies.
func New(d Deps) *API { return &API{deps: d} }

// ctxKey is the type for request-scoped context values.
type ctxKey int

const ctxAdminUser ctxKey = iota

// Handler returns the fully-wired /api/v1 mux. Protected routes are wrapped in
// the admin bearer-token middleware; auth/login is public.
func (a *API) Handler() http.Handler {
	mux := http.NewServeMux()

	// Public.
	mux.HandleFunc("POST /api/v1/auth/login", a.handleLogin)
	// Node enrollment exchange: authenticated by the one-time enroll_token in the
	// body, NOT an admin session (two-phase enrollment, 02-transport §2).
	mux.HandleFunc("POST /api/v1/enroll/exchange", a.handleEnrollExchange)

	// Protected (admin bearer token).
	prot := func(pattern string, h http.HandlerFunc) {
		mux.Handle(pattern, a.requireAdmin(h))
	}
	prot("POST /api/v1/auth/logout", a.handleLogout)
	prot("GET /api/v1/auth/me", a.handleMe)

	prot("GET /api/v1/nodes", a.handleListNodes)
	prot("GET /api/v1/nodes/{node_id}", a.handleGetNode)
	prot("PATCH /api/v1/nodes/{node_id}", a.handlePatchNode)
	prot("DELETE /api/v1/nodes/{node_id}", a.handleDeleteNode)
	prot("POST /api/v1/nodes/{node_id}/mcp:enable", a.handleMCPEnable)
	prot("POST /api/v1/nodes/{node_id}/mcp:disable", a.handleMCPDisable)

	prot("GET /api/v1/nodes/{node_id}/activities", a.handleActivities)

	prot("POST /api/v1/nodes/{node_id}/tasks", a.handleCreateTask)
	prot("GET /api/v1/nodes/{node_id}/tasks", a.handleListTasks)
	prot("GET /api/v1/tasks/{task_id}", a.handleGetTask)

	prot("POST /api/v1/nodes/enroll", a.handleEnroll)
	prot("POST /api/v1/nodes/{node_id}/revoke", a.handleRevoke)

	return mux
}

// requireAdmin validates the Authorization: Bearer <token> header.
func (a *API) requireAdmin(next http.HandlerFunc) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		tok := bearerToken(r)
		if tok == "" {
			writeError(w, http.StatusUnauthorized, "unauthorized", "missing bearer token")
			return
		}
		user, err := a.deps.Auth.VerifyAdmin(tok)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "unauthorized", "invalid or expired token")
			return
		}
		ctx := context.WithValue(r.Context(), ctxAdminUser, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// bearerToken extracts the token from an Authorization header.
func bearerToken(r *http.Request) string {
	h := r.Header.Get("Authorization")
	const p = "Bearer "
	if len(h) > len(p) && strings.EqualFold(h[:len(p)], p) {
		return strings.TrimSpace(h[len(p):])
	}
	return ""
}

// ---- response helpers ----

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// errorBody is the uniform error envelope: {"error":{"code","message"}}.
type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func writeError(w http.ResponseWriter, status int, code, msg string) {
	var b errorBody
	b.Error.Code = code
	b.Error.Message = msg
	writeJSON(w, status, b)
}

// decodeJSON reads a JSON request body into v; returns false (and writes 400) on failure.
func decodeJSON(w http.ResponseWriter, r *http.Request, v any) bool {
	if err := json.NewDecoder(r.Body).Decode(v); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "invalid JSON body")
		return false
	}
	return true
}

// queryInt parses a query int with a default and a hard cap.
func queryInt(r *http.Request, key string, def, max int) int {
	s := r.URL.Query().Get(key)
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 {
		return def
	}
	if n > max {
		return max
	}
	return n
}
