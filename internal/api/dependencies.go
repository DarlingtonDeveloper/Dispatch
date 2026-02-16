package api

import (
	"encoding/json"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type DependenciesHandler struct {
	store store.Store
}

func NewDependenciesHandler(s store.Store) *DependenciesHandler {
	return &DependenciesHandler{store: s}
}

type CreateDependencyRequest struct {
	BlockerID string `json:"blocker_id"`
	BlockedID string `json:"blocked_id"`
}

// Create handles POST /api/v1/backlog/dependencies
func (h *DependenciesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateDependencyRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	blockerID, err := uuid.Parse(req.BlockerID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid blocker_id"})
		return
	}
	blockedID, err := uuid.Parse(req.BlockedID)
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid blocked_id"})
		return
	}

	dep := &store.BacklogDependency{
		BlockerID: blockerID,
		BlockedID: blockedID,
	}
	if err := h.store.CreateDependency(r.Context(), dep); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusCreated, dep)
}

// Delete handles DELETE /api/v1/backlog/dependencies/{id}
func (h *DependenciesHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	if err := h.store.DeleteDependency(r.Context(), id); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted"})
}

// ListForItem handles GET /api/v1/backlog/{id}/dependencies
func (h *DependenciesHandler) ListForItem(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	deps, err := h.store.GetDependenciesForItem(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if deps == nil {
		deps = []*store.BacklogDependency{}
	}
	writeJSON(w, http.StatusOK, deps)
}
