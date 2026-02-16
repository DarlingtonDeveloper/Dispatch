package api

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
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
			ItemID:    id.String(),
			ItemTitle: item.Title,
			FromStage: "",
			ToStage:   template[0],
			Tier:      item.ModelTier,
		})
	}

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"item":  item,
		"gates": gates,
	})
}

// SubmitEvidence handles POST /api/v1/backlog/{id}/gate/evidence
func (h *StagesHandler) SubmitEvidence(w http.ResponseWriter, r *http.Request) {
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
		Stage       string `json:"stage"`
		Criterion   string `json:"criterion"`
		Evidence    string `json:"evidence"`
		SubmittedBy string `json:"submitted_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Stage == "" || req.Criterion == "" || req.Evidence == "" || req.SubmittedBy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "stage, criterion, evidence, and submitted_by are required"})
		return
	}

	// Submit evidence
	if err := h.store.SubmitEvidence(r.Context(), id, req.Stage, req.Criterion, req.Evidence, req.SubmittedBy); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Check if this is an economy tier item with auto-approve enabled
	if item.ModelTier == "economy" {
		config, err := h.store.GetAutonomyConfig(r.Context(), "economy")
		if err == nil && config.AutoApprove {
			// Auto-satisfy this criterion
			if err := h.store.SatisfyCriterion(r.Context(), id, req.Stage, req.Criterion, "auto-approved"); err == nil {
				// Check if all criteria are now satisfied for auto-advance
				allMet, _ := h.store.AllCriteriaMet(r.Context(), id, req.Stage)
				if allMet {
					h.handleAutoAdvance(r.Context(), item, req.Stage)
				}
			}
		}
	}

	// Publish evidence event with enriched payload for Slack gateway
	if h.hermes != nil {
		// Build all_criteria snapshot for the Block Kit message
		allCriteria, _ := h.store.GetGateStatus(r.Context(), id, req.Stage)
		var criteriaSnapshot []hermes.GateEvidenceCriterion
		for _, c := range allCriteria {
			criteriaSnapshot = append(criteriaSnapshot, hermes.GateEvidenceCriterion{
				Name:        c.Criterion,
				Evidence:    c.Evidence,
				HasEvidence: c.Evidence != "",
			})
		}
		_ = h.hermes.Publish(hermes.SubjectGateEvidence(id.String()), hermes.GateEvidenceEvent{
			ItemID:      id.String(),
			ItemTitle:   item.Title,
			ModelTier:   item.ModelTier,
			Stage:       req.Stage,
			StageIndex:  item.StageIndex,
			TotalStages: len(item.StageTemplate),
			Criterion:   req.Criterion,
			Evidence:    req.Evidence,
			SubmittedBy: req.SubmittedBy,
			AgentID:     req.SubmittedBy,
			AllCriteria: criteriaSnapshot,
		})
	}

	// Return updated gate status
	criteria, _ := h.store.GetGateStatus(r.Context(), id, req.Stage)
	allMet, _ := h.store.AllCriteriaMet(r.Context(), id, req.Stage)

	writeJSON(w, http.StatusCreated, map[string]interface{}{
		"stage":    req.Stage,
		"criteria": criteria,
		"all_met":  allMet,
	})
}

// SatisfyGate handles POST /api/v1/backlog/{id}/gate/satisfy (Admin only)
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
		Decision    string `json:"decision,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	stage := item.CurrentStage
	
	// Handle economy tier autonomy counter logic
	if item.ModelTier == "economy" {
		if strings.ToLower(req.Decision) == "approved" {
			// Increment consecutive approvals
			count, err := h.store.IncrementConsecutiveApprovals(r.Context(), "economy")
			if err == nil && count >= 20 {
				// Graduate to auto-approve
				_ = h.store.UpdateAutonomyConfig(r.Context(), "economy", true, count, 0)
				if h.hermes != nil {
					_ = h.hermes.Publish(hermes.SubjectAutonomyGraduated(), hermes.AutonomyGraduatedEvent{
						Tier:          "economy",
						Threshold:     20,
						ApprovedCount: count,
					})
				}
			}
		} else if strings.ToLower(req.Decision) == "rejected" || strings.Contains(strings.ToLower(req.Decision), "change") {
			// Reset counter on rejection/changes
			_ = h.store.ResetAutonomyCounters(r.Context(), "economy")
		}
	}

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

	// Publish gate satisfied event
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

	// Check if all criteria are now satisfied for auto-advance
	allMet, _ := h.store.AllCriteriaMet(r.Context(), id, stage)
	if allMet {
		h.handleAutoAdvance(r.Context(), item, stage)
		// Refresh item after potential advancement
		item, _ = h.store.GetBacklogItem(r.Context(), id)
	}

	criteria, _ := h.store.GetGateStatus(r.Context(), id, stage)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stage":    stage,
		"criteria": criteria,
		"all_met":  allMet,
		"item":     item,
	})
}

// RequestChanges handles POST /api/v1/backlog/{id}/gate/request-changes (Admin only)
func (h *StagesHandler) RequestChanges(w http.ResponseWriter, r *http.Request) {
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
		Stage       string `json:"stage"`
		Feedback    string `json:"feedback"`
		RequestedBy string `json:"requested_by"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if req.Stage == "" {
		req.Stage = item.CurrentStage
	}

	if req.Feedback == "" || req.RequestedBy == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "feedback and requested_by are required"})
		return
	}

	// Reset stage to active (unsatisfy all criteria)
	if err := h.store.ResetStageToActive(r.Context(), id, req.Stage); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Handle economy tier autonomy counter (reset on change request)
	if item.ModelTier == "economy" {
		_ = h.store.ResetAutonomyCounters(r.Context(), "economy")
	}

	// Publish changes requested event
	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectGateChangesRequested(id.String()), hermes.GateChangesRequestedEvent{
			ItemID:      id.String(),
			Stage:       req.Stage,
			Feedback:    req.Feedback,
			RequestedBy: req.RequestedBy,
		})
	}

	// Return updated gate status
	criteria, _ := h.store.GetGateStatus(r.Context(), id, req.Stage)
	allMet, _ := h.store.AllCriteriaMet(r.Context(), id, req.Stage)

	writeJSON(w, http.StatusOK, map[string]interface{}{
		"stage":    req.Stage,
		"criteria": criteria,
		"all_met":  allMet,
		"feedback": req.Feedback,
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

// handleAutoAdvance advances to next stage when all criteria are met
func (h *StagesHandler) handleAutoAdvance(ctx context.Context, item *store.BacklogItem, currentStage string) {
	// Check if at last stage
	if item.StageIndex >= len(item.StageTemplate)-1 {
		// Mark as completed
		item.Status = store.BacklogStatusDone
		_ = h.store.UpdateBacklogItem(ctx, item)

		// Publish completion event
		if h.hermes != nil {
			_ = h.hermes.Publish(hermes.SubjectItemCompleted(item.ID.String()), hermes.ItemCompletedEvent{
				ItemID:          item.ID.String(),
				Title:           item.Title,
				StagesCompleted: len(item.StageTemplate),
				TotalDurationMs: time.Since(item.CreatedAt).Milliseconds(),
			})
		}
		return
	}

	// Advance to next stage
	previousStage := item.CurrentStage
	item.StageIndex++
	item.CurrentStage = item.StageTemplate[item.StageIndex]

	if err := h.store.UpdateBacklogItem(ctx, item); err != nil {
		return
	}

	// Publish stage advancement
	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectStageAdvanced(item.ID.String()), hermes.StageAdvancedEvent{
			ItemID:     item.ID.String(),
			ItemTitle:  item.Title,
			FromStage:  previousStage,
			ToStage:    item.CurrentStage,
			StageIndex: item.StageIndex,
			Tier:       item.ModelTier,
		})
	}
}
