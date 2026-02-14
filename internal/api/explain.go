package api

import (
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type ExplainHandler struct {
	store store.Store
}

func NewExplainHandler(s store.Store) *ExplainHandler {
	return &ExplainHandler{store: s}
}

// Explain returns the scoring breakdown for a task.
// GET /api/v1/scoring/explain/{task_id}
func (h *ExplainHandler) Explain(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "task_id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task_id"})
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	resp := map[string]interface{}{
		"task_id":         task.ID,
		"scoring_version": task.ScoringVersion,
		"oversight_level": task.OversightLevel,
		"fast_path":       task.FastPath,
		"assigned_agent":  task.AssignedAgent,
	}

	if task.ScoringFactors != nil {
		resp["scoring_factors"] = task.ScoringFactors
	}
	if task.ParetoFrontier != nil {
		resp["pareto_frontier"] = task.ParetoFrontier
	}

	writeJSON(w, http.StatusOK, resp)
}
