package api

import (
	"encoding/json"
	"net/http"

	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type OverridesHandler struct {
	store  store.Store
	hermes hermes.Client
}

func NewOverridesHandler(s store.Store, h hermes.Client) *OverridesHandler {
	return &OverridesHandler{store: s, hermes: h}
}

type CreateOverrideRequest struct {
	BacklogItemID string `json:"backlog_item_id,omitempty"`
	TaskID        string `json:"task_id,omitempty"`
	OverrideType  string `json:"override_type"`
	PreviousValue string `json:"previous_value,omitempty"`
	NewValue      string `json:"new_value"`
	Reason        string `json:"reason,omitempty"`
	OverriddenBy  string `json:"overridden_by"`
}

// Create handles POST /api/v1/overrides
func (h *OverridesHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateOverrideRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.OverrideType == "" || req.NewValue == "" || req.OverriddenBy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "override_type, new_value, and overridden_by required"})
		return
	}

	override := &store.DispatchOverride{
		OverrideType:  req.OverrideType,
		PreviousValue: req.PreviousValue,
		NewValue:      req.NewValue,
		Reason:        req.Reason,
		OverriddenBy:  req.OverriddenBy,
	}

	if req.BacklogItemID != "" {
		id, err := uuid.Parse(req.BacklogItemID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid backlog_item_id"})
			return
		}
		override.BacklogItemID = &id
	}
	if req.TaskID != "" {
		id, err := uuid.Parse(req.TaskID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task_id"})
			return
		}
		override.TaskID = &id
	}

	if err := h.store.CreateOverride(r.Context(), override); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Record autonomy event
	_ = h.store.CreateAutonomyEvent(r.Context(), &store.AutonomyEvent{
		BacklogItemID: override.BacklogItemID,
		TaskID:        override.TaskID,
		EventType:     "overridden",
		WasAutonomous: false,
		Details: map[string]interface{}{
			"override_type":  req.OverrideType,
			"previous_value": req.PreviousValue,
			"new_value":      req.NewValue,
			"reason":         req.Reason,
		},
	})

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectOverrideRecorded(override.ID.String()), hermes.OverrideRecordedEvent{
			OverrideID:   override.ID.String(),
			OverrideType: req.OverrideType,
			OverriddenBy: req.OverriddenBy,
			NewValue:     req.NewValue,
		})
	}

	writeJSON(w, http.StatusCreated, override)
}
