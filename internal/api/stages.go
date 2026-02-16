package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type StagesHandler struct {
	store  store.Store
	hermes hermes.Client
	cfg    *config.Config
}

func NewStagesHandler(s store.Store, h hermes.Client, cfg *config.Config) *StagesHandler {
	return &StagesHandler{store: s, hermes: h, cfg: cfg}
}

// InitStages handles POST /api/v1/backlog/{id}/init-stages
func (h *StagesHandler) InitStages(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil || item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}

	var req struct {
		Template []string `json:"template,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	template := req.Template
	if len(template) == 0 {
		tier := item.ModelTier
		if tier == "" {
			tier = "standard"
		}
		if t, ok := store.StageTemplates[tier]; ok {
			template = t
		} else {
			template = store.StageTemplates["standard"]
		}
	}

	if err := h.store.InitStages(r.Context(), id, template); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Determine gate criteria per stage based on tier
	isEconomy := item.ModelTier == "economy"
	for _, stage := range template {
		var criteria []string
		if isEconomy {
			// Economy tier gets minimal criteria
			switch stage {
			case "implement":
				criteria = []string{"code complete"}
			case "verify":
				criteria = []string{"tests passing"}
			default:
				if c, ok := h.cfg.StageGates.Gates[stage]; ok {
					criteria = c
				}
			}
		} else {
			if c, ok := h.cfg.StageGates.Gates[stage]; ok {
				criteria = c
			}
		}
		if len(criteria) > 0 {
			if err := h.store.CreateGateCriteria(r.Context(), id, stage, criteria); err != nil {
				writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
				return
			}
		}
	}

	// Re-read the item to get updated fields
	item, _ = h.store.GetBacklogItem(r.Context(), id)

	// Build response with all gates
	type stageGate struct {
		Stage    string               `json:"stage"`
		Criteria []store.GateCriterion `json:"criteria"`
	}
	var gates []stageGate
	for _, stage := range template {
		criteria, _ := h.store.GetGateStatus(r.Context(), id, stage)
		if criteria == nil {
			criteria = []store.GateCriterion{}
		}
		gates = append(gates, stageGate{Stage: stage, Criteria: criteria})
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectStageAdvanced(id.String()), hermes.StageAdvancedEvent{
			ItemID:        id.String(),
			PreviousStage: "",
			CurrentStage:  template[0],
			Tier:          item.ModelTier,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"item":  item,
		"gates": gates,
	})
}

// AdvanceStage handles POST /api/v1/backlog/{id}/advance-stage
func (h *StagesHandler) AdvanceStage(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil || item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}

	if len(item.StageTemplate) == 0 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "stages not initialized"})
		return
	}

	var req struct {
		Force  bool   `json:"force"`
		Reason string `json:"reason"`
	}
	_ = json.NewDecoder(r.Body).Decode(&req)

	if req.Force && req.Reason == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "force requires reason"})
		return
	}

	if !req.Force {
		allMet, err := h.store.AllCriteriaMet(r.Context(), id, item.CurrentStage)
		if err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
		if !allMet {
			criteria, _ := h.store.GetGateStatus(r.Context(), id, item.CurrentStage)
			var unmet []string
			for _, c := range criteria {
				if !c.Satisfied {
					unmet = append(unmet, c.Criterion)
				}
			}
			writeJSON(w, http.StatusConflict, map[string]interface{}{
				"error":        "unmet gate criteria",
				"unmet":        unmet,
				"current_stage": item.CurrentStage,
			})
			return
		}
	}

	// Velocity check: if stage lasted < 10s and 0 criteria were satisfied manually, reject
	if !req.Force {
		criteria, _ := h.store.GetGateStatus(r.Context(), id, item.CurrentStage)
		manuallySatisfied := 0
		for _, c := range criteria {
			if c.Satisfied && c.SatisfiedBy != "" {
				manuallySatisfied++
			}
		}
		if manuallySatisfied == 0 && len(criteria) > 0 {
			// Check if stage was created recently (within 10s)
			if item.UpdatedAt.After(time.Now().Add(-10 * time.Second)) {
				writeJSON(w, http.StatusConflict, map[string]string{"error": "velocity check failed: stage advanced too quickly with no manual gate satisfactions"})
				return
			}
		}
	}

	// Check if at last stage
	if item.StageIndex >= len(item.StageTemplate)-1 {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "already at final stage"})
		return
	}

	previousStage := item.CurrentStage
	item.StageIndex++
	item.CurrentStage = item.StageTemplate[item.StageIndex]

	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectStageAdvanced(id.String()), hermes.StageAdvancedEvent{
			ItemID:        id.String(),
			PreviousStage: previousStage,
			CurrentStage:  item.CurrentStage,
			Tier:          item.ModelTier,
		})

		// If we just advanced to the last stage, also publish completed
		if item.StageIndex == len(item.StageTemplate)-1 {
			_ = h.hermes.Publish(hermes.SubjectStageCompleted(id.String()), hermes.StageCompletedEvent{
				ItemID:      id.String(),
				Tier:        item.ModelTier,
				TotalStages: len(item.StageTemplate),
			})
		}
	}

	writeJSON(w, http.StatusOK, item)
}

// SatisfyGate handles POST /api/v1/backlog/{id}/gate/satisfy
func (h *StagesHandler) SatisfyGate(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil || item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}

	var req struct {
		Criterion   string `json:"criterion"`
		All         bool   `json:"all"`
		SatisfiedBy string `json:"satisfied_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	stage := item.CurrentStage
	if req.All {
		if err := h.store.SatisfyAllCriteria(r.Context(), id, stage, req.SatisfiedBy); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	} else {
		if req.Criterion == "" {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "criterion required when all is false"})
			return
		}
		if err := h.store.SatisfyCriterion(r.Context(), id, stage, req.Criterion, req.SatisfiedBy); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	if h.hermes != nil {
		criterion := req.Criterion
		if req.All {
			criterion = "*"
		}
		_ = h.hermes.Publish(hermes.SubjectGateSatisfied(id.String()), hermes.GateSatisfiedEvent{
			ItemID:      id.String(),
			Stage:       stage,
			Criterion:   criterion,
			SatisfiedBy: req.SatisfiedBy,
		})
	}

	criteria, _ := h.store.GetGateStatus(r.Context(), id, stage)
	allMet, _ := h.store.AllCriteriaMet(r.Context(), id, stage)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stage":    stage,
		"criteria": criteria,
		"all_met":  allMet,
	})
}

// GateStatus handles GET /api/v1/backlog/{id}/gate/status
func (h *StagesHandler) GateStatus(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil || item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}

	stage := r.URL.Query().Get("stage")
	if stage == "" {
		stage = item.CurrentStage
	}

	criteria, err := h.store.GetGateStatus(r.Context(), id, stage)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if criteria == nil {
		criteria = []store.GateCriterion{}
	}

	allMet, _ := h.store.AllCriteriaMet(r.Context(), id, stage)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stage":    stage,
		"criteria": criteria,
		"all_met":  allMet,
	})
}
