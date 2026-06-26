package api

import (
	"net/http"
	"strconv"

	"github.com/sit/sit/internal/manager/store"
)

// GET /api/v1/nodes/{node_id}/activities?limit=&before=
func (a *API) handleActivities(w http.ResponseWriter, r *http.Request) {
	nodeID := r.PathValue("node_id")
	limit := queryInt(r, "limit", 50, 500)
	var before int64
	if s := r.URL.Query().Get("before"); s != "" {
		if n, err := strconv.ParseInt(s, 10, 64); err == nil {
			before = n
		}
	}
	acts, err := a.deps.Store.ListActivities(r.Context(), nodeID, limit, before)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "list activities failed")
		return
	}
	if acts == nil {
		acts = []store.Activity{}
	}
	writeJSON(w, http.StatusOK, map[string]any{"activities": acts})
}
