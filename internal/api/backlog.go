package api

import (
	"encoding/json"
	"net/http"
	"strconv"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type BacklogHandler struct {
	store  store.Store
	hermes hermes.Client
	scorer *scoring.BacklogScorer
}

func NewBacklogHandler(s store.Store, h hermes.Client, bs *scoring.BacklogScorer) *BacklogHandler {
	return &BacklogHandler{store: s, hermes: h, scorer: bs}
}

type CreateBacklogItemRequest struct {
	Title           string                 `json:"title"`
	Description     string                 `json:"description,omitempty"`
	ItemType        string                 `json:"item_type,omitempty"`
	Domain          string                 `json:"domain,omitempty"`
	AssignedTo      string                 `json:"assigned_to,omitempty"`
	ParentID        string                 `json:"parent_id,omitempty"`
	Impact          *float64               `json:"impact,omitempty"`
	ManualPriority  *float64               `json:"manual_priority,omitempty"`
	Urgency         *float64               `json:"urgency,omitempty"`
	EstimatedTokens *int64                 `json:"estimated_tokens,omitempty"`
	EffortEstimate  string                 `json:"effort_estimate,omitempty"`
	Labels          []string               `json:"labels,omitempty"`
	OneWayDoor      bool                   `json:"one_way_door"`
	Source          string                 `json:"source,omitempty"`
	Metadata        map[string]interface{} `json:"metadata,omitempty"`
}

// Create handles POST /api/v1/backlog
func (h *BacklogHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateBacklogItemRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}

	item := &store.BacklogItem{
		Title:           req.Title,
		Description:     req.Description,
		ItemType:        req.ItemType,
		Status:          store.BacklogStatusBacklog,
		Domain:          req.Domain,
		AssignedTo:      req.AssignedTo,
		Impact:          req.Impact,
		EstimatedTokens: req.EstimatedTokens,
		EffortEstimate:  req.EffortEstimate,
		Labels:          req.Labels,
		OneWayDoor:      req.OneWayDoor,
		Source:          req.Source,
		ScoresSource:    "manual",
		Metadata:        req.Metadata,
	}
	if item.ItemType == "" {
		item.ItemType = "task"
	}
	if item.Source == "" {
		item.Source = "manual"
	}

	// Map manual_priority → urgency if urgency not provided
	if req.ManualPriority != nil && req.Urgency == nil {
		item.Urgency = req.ManualPriority
	} else {
		item.Urgency = req.Urgency
	}

	if req.ParentID != "" {
		pid, err := uuid.Parse(req.ParentID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parent_id"})
			return
		}
		item.ParentID = &pid
	}

	// Initial scoring
	if h.scorer != nil {
		hasBlockers, _ := h.store.HasUnresolvedBlockers(r.Context(), uuid.Nil)
		medianTokens, _ := h.store.GetMedianEstimatedTokens(r.Context())
		score := h.scorer.ScoreItem(item, hasBlockers, medianTokens)
		item.PriorityScore = &score
	}

	if err := h.store.CreateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogCreated(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusCreated, item)
}

// List handles GET /api/v1/backlog
func (h *BacklogHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := store.BacklogFilter{
		Domain:     r.URL.Query().Get("domain"),
		AssignedTo: r.URL.Query().Get("assigned_to"),
		ItemType:   r.URL.Query().Get("item_type"),
	}
	if s := r.URL.Query().Get("status"); s != "" {
		status := store.BacklogStatus(s)
		filter.Status = &status
	}
	if s := r.URL.Query().Get("parent_id"); s != "" {
		pid, err := uuid.Parse(s)
		if err == nil {
			filter.ParentID = &pid
		}
	}
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			filter.Limit = n
		}
	}
	if s := r.URL.Query().Get("offset"); s != "" {
		if n, err := strconv.Atoi(s); err == nil {
			filter.Offset = n
		}
	}

	items, err := h.store.ListBacklogItems(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if items == nil {
		items = []*store.BacklogItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// Next handles GET /api/v1/backlog/next — live re-scoring of ready items
func (h *BacklogHandler) Next(w http.ResponseWriter, r *http.Request) {
	limit := 10
	if s := r.URL.Query().Get("limit"); s != "" {
		if n, err := strconv.Atoi(s); err == nil && n > 0 {
			limit = n
		}
	}

	items, err := h.store.GetNextBacklogItems(r.Context(), limit)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Live re-score
	if h.scorer != nil {
		medianTokens, _ := h.store.GetMedianEstimatedTokens(r.Context())
		for _, item := range items {
			hasBlockers, _ := h.store.HasUnresolvedBlockers(r.Context(), item.ID)
			score := h.scorer.ScoreItem(item, hasBlockers, medianTokens)
			item.PriorityScore = &score
		}
	}

	if items == nil {
		items = []*store.BacklogItem{}
	}
	writeJSON(w, http.StatusOK, items)
}

// Get handles GET /api/v1/backlog/{id}
func (h *BacklogHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}
	writeJSON(w, http.StatusOK, item)
}

// Update handles PATCH /api/v1/backlog/{id}
func (h *BacklogHandler) Update(w http.ResponseWriter, r *http.Request) {
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

	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if v, ok := patch["title"].(string); ok && v != "" {
		item.Title = v
	}
	if v, ok := patch["description"].(string); ok {
		item.Description = v
	}
	if v, ok := patch["domain"].(string); ok {
		item.Domain = v
	}
	if v, ok := patch["assigned_to"].(string); ok {
		item.AssignedTo = v
	}
	if v, ok := patch["item_type"].(string); ok {
		item.ItemType = v
	}
	if v, ok := patch["effort_estimate"].(string); ok {
		item.EffortEstimate = v
	}
	if v, ok := patch["impact"].(float64); ok {
		item.Impact = &v
	}
	if v, ok := patch["urgency"].(float64); ok {
		item.Urgency = &v
	}
	if v, ok := patch["status"].(string); ok {
		item.Status = store.BacklogStatus(v)
	}
	if v, ok := patch["labels"].([]interface{}); ok {
		labels := make([]string, 0, len(v))
		for _, l := range v {
			if s, ok := l.(string); ok {
				labels = append(labels, s)
			}
		}
		item.Labels = labels
	}
	if v, ok := patch["one_way_door"].(bool); ok {
		item.OneWayDoor = v
	}
	if v, ok := patch["metadata"].(map[string]interface{}); ok {
		item.Metadata = v
	}

	// Re-score on update
	if h.scorer != nil {
		hasBlockers, _ := h.store.HasUnresolvedBlockers(r.Context(), item.ID)
		medianTokens, _ := h.store.GetMedianEstimatedTokens(r.Context())
		score := h.scorer.ScoreItem(item, hasBlockers, medianTokens)
		item.PriorityScore = &score
	}

	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogUpdated(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// Delete handles DELETE /api/v1/backlog/{id} — sets status to cancelled
func (h *BacklogHandler) Delete(w http.ResponseWriter, r *http.Request) {
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

	item.Status = store.BacklogStatusCancelled
	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogCancelled(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// Start handles POST /api/v1/backlog/{id}/start — ready→in_discovery
func (h *BacklogHandler) Start(w http.ResponseWriter, r *http.Request) {
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

	if item.Status != store.BacklogStatusReady {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "item must be in ready status"})
		return
	}

	item.Status = store.BacklogStatusInDiscovery
	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogStarted(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// DiscoveryComplete handles PATCH /api/v1/backlog/{id}/discovery-complete
func (h *BacklogHandler) DiscoveryComplete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid id"})
		return
	}

	// Verify item is in discovery
	item, err := h.store.GetBacklogItem(r.Context(), id)
	if err != nil || item == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "item not found"})
		return
	}
	if item.Status != store.BacklogStatusInDiscovery {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "item must be in in_discovery status"})
		return
	}

	var req store.BacklogDiscoveryCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	var scoreFn store.ScoreFn
	if h.scorer != nil {
		scoreFn = h.scorer.ScoreItem
	}

	// tierFn derives model tier from item properties
	tierFn := func(item *store.BacklogItem) string {
		if item.OneWayDoor {
			return "premium"
		}
		if item.Impact != nil && *item.Impact >= 0.8 {
			return "premium"
		}
		if item.EstimatedTokens != nil && *item.EstimatedTokens < 5000 {
			return "economy"
		}
		return "standard"
	}

	result, err := h.store.BacklogDiscoveryComplete(r.Context(), id, &req, scoreFn, tierFn)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogPlanned(id.String()), hermes.BacklogDiscoveryCompleteEvent{
			ItemID:        id.String(),
			Status:        string(result.Item.Status),
			PreviousScore: result.PreviousScore,
			UpdatedScore:  result.UpdatedScore,
			ModelTier:     result.ModelTier,
			SubtaskCount:  len(result.CreatedSubtasks),
		})
	}

	writeJSON(w, http.StatusOK, result)
}

// BeginExecution handles POST /api/v1/backlog/{id}/begin-execution — planned→in_progress
func (h *BacklogHandler) BeginExecution(w http.ResponseWriter, r *http.Request) {
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

	if item.Status != store.BacklogStatusPlanned {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "item must be in planned status"})
		return
	}

	item.Status = store.BacklogStatusInProgress
	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogExecuting(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// Complete handles POST /api/v1/backlog/{id}/complete — resolves deps for blocked items
func (h *BacklogHandler) Complete(w http.ResponseWriter, r *http.Request) {
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

	if item.Status != store.BacklogStatusInProgress && item.Status != store.BacklogStatusReview {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "item must be in in_progress or review status"})
		return
	}

	item.Status = store.BacklogStatusDone
	now := time.Now()
	_ = now
	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Resolve dependencies where this item is the blocker
	_ = h.store.ResolveDependenciesForBlocker(r.Context(), id)

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogCompleted(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// Block handles POST /api/v1/backlog/{id}/block
func (h *BacklogHandler) Block(w http.ResponseWriter, r *http.Request) {
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

	var body struct {
		Reason string `json:"reason,omitempty"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)

	item.Status = store.BacklogStatusBlocked
	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogBlocked(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}

// Park handles POST /api/v1/backlog/{id}/park — returns to backlog with updated scores
func (h *BacklogHandler) Park(w http.ResponseWriter, r *http.Request) {
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

	item.Status = store.BacklogStatusBacklog

	// Re-score
	if h.scorer != nil {
		hasBlockers, _ := h.store.HasUnresolvedBlockers(r.Context(), item.ID)
		medianTokens, _ := h.store.GetMedianEstimatedTokens(r.Context())
		score := h.scorer.ScoreItem(item, hasBlockers, medianTokens)
		item.PriorityScore = &score
	}

	if err := h.store.UpdateBacklogItem(r.Context(), item); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectBacklogParked(item.ID.String()), hermes.BacklogItemEvent{
			ItemID: item.ID.String(),
			Status: string(item.Status),
			Title:  item.Title,
		})
	}

	writeJSON(w, http.StatusOK, item)
}
