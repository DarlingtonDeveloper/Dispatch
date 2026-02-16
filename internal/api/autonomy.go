package api

import (
	"net/http"
	"strconv"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type AutonomyHandler struct {
	store store.Store
}

func NewAutonomyHandler(s store.Store) *AutonomyHandler {
	return &AutonomyHandler{store: s}
}

// Metrics handles GET /api/v1/autonomy/metrics
func (h *AutonomyHandler) Metrics(w http.ResponseWriter, r *http.Request) {
	days := 30
	if s := r.URL.Query().Get("days"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			days = n
		}
	}

	metrics, err := h.store.GetAutonomyMetrics(r.Context(), days)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if metrics == nil {
		metrics = []*store.AutonomyMetrics{}
	}
	writeJSON(w, http.StatusOK, metrics)
}
