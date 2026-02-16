package api

import (
	"encoding/json"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

type TasksHandler struct {
	store        store.Store
	hermes       hermes.Client
	modelRouting config.ModelRoutingConfig
}

func NewTasksHandler(s store.Store, h hermes.Client, mr config.ModelRoutingConfig) *TasksHandler {
	return &TasksHandler{store: s, hermes: h, modelRouting: mr}
}

type CreateTaskRequest struct {
	Title                string                 `json:"title"`
	Description          string                 `json:"description,omitempty"`
	Owner                string                 `json:"owner"`
	RequiredCapabilities []string               `json:"required_capabilities,omitempty"`
	Priority             int                    `json:"priority,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	TimeoutSeconds       int                    `json:"timeout_seconds,omitempty"`
	MaxRetries           int                    `json:"max_retries,omitempty"`
	Source               string                 `json:"source,omitempty"`
	ParentTaskID         string                 `json:"parent_task_id,omitempty"`
}

func (h *TasksHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req CreateTaskRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}
	if req.Title == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "title required"})
		return
	}

	owner := req.Owner
	if owner == "" {
		owner = r.Header.Get("X-Agent-ID")
	}
	if owner == "" {
		owner = "system"
	}

	source := req.Source
	if source == "" {
		if agentID := r.Header.Get("X-Agent-ID"); agentID != "" {
			source = "agent"
		} else {
			source = "manual"
		}
	}

	task := &store.Task{
		Title:                req.Title,
		Description:          req.Description,
		Owner:                owner,
		RequiredCapabilities: req.RequiredCapabilities,
		Priority:             req.Priority,
		Status:               store.StatusPending,
		Metadata:             req.Metadata,
		TimeoutSeconds:       req.TimeoutSeconds,
		MaxRetries:           req.MaxRetries,
		Source:               source,
		RetryEligible:        true,
	}
	if task.TimeoutSeconds == 0 {
		task.TimeoutSeconds = 300
	}
	if task.MaxRetries == 0 {
		task.MaxRetries = 3
	}
	if req.ParentTaskID != "" {
		pid, err := uuid.Parse(req.ParentTaskID)
		if err != nil {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid parent_task_id"})
			return
		}
		task.ParentTaskID = &pid
	}

	if err := h.store.CreateTask(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectTaskCreated(task.ID.String()), task)
	}

	writeJSON(w, http.StatusCreated, task)
}

func (h *TasksHandler) List(w http.ResponseWriter, r *http.Request) {
	filter := store.TaskFilter{
		Agent:  r.URL.Query().Get("agent"),
		Owner:  r.URL.Query().Get("owner"),
		Source: r.URL.Query().Get("source"),
	}
	if s := r.URL.Query().Get("status"); s != "" {
		status := store.TaskStatus(s)
		filter.Status = &status
	}

	tasks, err := h.store.ListTasks(r.Context(), filter)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if tasks == nil {
		tasks = []*store.Task{}
	}
	writeJSON(w, http.StatusOK, tasks)
}

func (h *TasksHandler) Get(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
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
	writeJSON(w, http.StatusOK, task)
}

func (h *TasksHandler) Update(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	var patch map[string]interface{}
	if err := json.NewDecoder(r.Body).Decode(&patch); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	if metadata, ok := patch["metadata"].(map[string]interface{}); ok {
		task.Metadata = metadata
	}

	if err := h.store.UpdateTask(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, task)
}

func (h *TasksHandler) Complete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	var body struct {
		Result map[string]interface{} `json:"result"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	now := time.Now()
	task.Status = store.StatusCompleted
	task.Result = body.Result
	task.CompletedAt = &now

	if err := h.store.UpdateTask(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	_ = h.store.CreateTaskEvent(r.Context(), &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "completed",
		AgentID: r.Header.Get("X-Agent-ID"),
	})

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectTaskCompleted(task.ID.String()), hermes.TaskCompletedEvent{
			TaskID: task.ID.String(),
			Result: body.Result,
		})
	}

	writeJSON(w, http.StatusOK, task)
}

func (h *TasksHandler) Fail(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	var body struct {
		Error         string `json:"error"`
		RetryEligible *bool  `json:"retry_eligible,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	task.Status = store.StatusFailed
	task.Error = body.Error
	if body.RetryEligible != nil {
		task.RetryEligible = *body.RetryEligible
	}

	if err := h.store.UpdateTask(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	_ = h.store.CreateTaskEvent(r.Context(), &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "failed",
		AgentID: r.Header.Get("X-Agent-ID"),
	})

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectTaskFailed(task.ID.String()), hermes.TaskFailedEvent{
			TaskID:        task.ID.String(),
			Error:         body.Error,
			RetryEligible: task.RetryEligible,
		})
	}

	writeJSON(w, http.StatusOK, task)
}

func (h *TasksHandler) Progress(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
		return
	}

	task, err := h.store.GetTask(r.Context(), id)
	if err != nil || task == nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": "task not found"})
		return
	}

	if task.Status == store.StatusAssigned {
		now := time.Now()
		task.Status = store.StatusInProgress
		task.StartedAt = &now
		if err := h.store.UpdateTask(r.Context(), task); err != nil {
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
			return
		}
	}

	var body map[string]interface{}
	_ = json.NewDecoder(r.Body).Decode(&body)

	_ = h.store.CreateTaskEvent(r.Context(), &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "progress",
		AgentID: r.Header.Get("X-Agent-ID"),
		Payload: body,
	})

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectTaskProgress(task.ID.String()), body)
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

type DiscoveryCompleteRequest struct {
	ComplexityScore    *float64 `json:"complexity_score,omitempty"`
	RiskScore          *float64 `json:"risk_score,omitempty"`
	ReversibilityScore *float64 `json:"reversibility_score,omitempty"`
	OneWayDoor         *bool    `json:"one_way_door,omitempty"`
	Labels             []string `json:"labels,omitempty"`
	FilePatterns       []string `json:"file_patterns,omitempty"`
}

// DiscoveryComplete updates task scores after MC Discovery reveals true complexity,
// then re-derives model tier.
// PATCH /api/v1/tasks/{id}/discovery-complete
func (h *TasksHandler) DiscoveryComplete(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid task id"})
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

	// Only allow discovery-complete on assigned or in_progress tasks
	if task.Status != store.StatusAssigned && task.Status != store.StatusInProgress {
		writeJSON(w, http.StatusConflict, map[string]string{"error": "task must be assigned or in_progress"})
		return
	}

	var req DiscoveryCompleteRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid request body"})
		return
	}

	// Apply updated scores
	if req.ComplexityScore != nil {
		task.ComplexityScore = req.ComplexityScore
	}
	if req.RiskScore != nil {
		task.RiskScore = req.RiskScore
	}
	if req.ReversibilityScore != nil {
		task.ReversibilityScore = req.ReversibilityScore
	}
	if req.OneWayDoor != nil {
		task.OneWayDoor = *req.OneWayDoor
	}
	if req.Labels != nil {
		task.Labels = req.Labels
	}
	if req.FilePatterns != nil {
		task.FilePatterns = req.FilePatterns
	}

	// Re-derive model tier from updated scores
	if h.modelRouting.Enabled {
		tier := scoring.DeriveModelTier(task, h.modelRouting, false)
		task.ModelTier = tier.Name
		task.RoutingMethod = tier.RoutingMethod
		task.Runtime = scoring.RuntimeForTier(tier.Name, len(task.FilePatterns))
		if len(tier.Models) > 0 {
			task.RecommendedModel = tier.Models[0]
		}
	}

	if err := h.store.UpdateTask(r.Context(), task); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	_ = h.store.CreateTaskEvent(r.Context(), &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "discovery_complete",
		AgentID: r.Header.Get("X-Agent-ID"),
	})

	if h.hermes != nil {
		_ = h.hermes.Publish(hermes.SubjectTaskProgress(task.ID.String()), map[string]interface{}{
			"event":             "discovery_complete",
			"task_id":           task.ID.String(),
			"model_tier":        task.ModelTier,
			"recommended_model": task.RecommendedModel,
			"runtime":           task.Runtime,
		})
	}

	writeJSON(w, http.StatusOK, task)
}

func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}
