package broker

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/alexandria"
	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

type Broker struct {
	store      store.Store
	hermes     hermes.Client
	warren     warren.Client
	forge      forge.Client
	alexandria alexandria.Client
	scorer     *scoring.Scorer
	cfg        *config.Config
	logger     *slog.Logger

	drainedMu sync.RWMutex
	drained   map[string]bool

	stopOnce sync.Once
	stopCh   chan struct{}
	wg       sync.WaitGroup
}

func New(s store.Store, h hermes.Client, w warren.Client, f forge.Client, a alexandria.Client, cfg *config.Config, logger *slog.Logger) *Broker {
	weights := scoring.WeightSet{
		Capability:     cfg.Scoring.Weights.Capability,
		Availability:   cfg.Scoring.Weights.Availability,
		RiskFit:        cfg.Scoring.Weights.RiskFit,
		CostEfficiency: cfg.Scoring.Weights.CostEfficiency,
		Verifiability:  cfg.Scoring.Weights.Verifiability,
		Reversibility:  cfg.Scoring.Weights.Reversibility,
		ComplexityFit:  cfg.Scoring.Weights.ComplexityFit,
		UncertaintyFit: cfg.Scoring.Weights.UncertaintyFit,
		DurationFit:    cfg.Scoring.Weights.DurationFit,
		Contextuality:  cfg.Scoring.Weights.Contextuality,
		Subjectivity:   cfg.Scoring.Weights.Subjectivity,
	}
	sc := scoring.NewScorer(weights, cfg.Scoring.FastPathEnabled, logger)

	return &Broker{
		store:      s,
		hermes:     h,
		warren:     w,
		forge:      f,
		alexandria: a,
		scorer:     sc,
		cfg:        cfg,
		logger:     logger,
		drained:    make(map[string]bool),
		stopCh:     make(chan struct{}),
	}
}

func (b *Broker) Start(ctx context.Context) {
	b.wg.Add(2)
	go b.assignmentLoop(ctx)
	go b.timeoutLoop(ctx)
}

func (b *Broker) Stop() {
	b.stopOnce.Do(func() { close(b.stopCh) })
	b.wg.Wait()
}

func (b *Broker) DrainAgent(agentID string) {
	b.drainedMu.Lock()
	b.drained[agentID] = true
	b.drainedMu.Unlock()
}

func (b *Broker) UndrainAgent(agentID string) {
	b.drainedMu.Lock()
	delete(b.drained, agentID)
	b.drainedMu.Unlock()
}

func (b *Broker) IsDrained(agentID string) bool {
	b.drainedMu.RLock()
	defer b.drainedMu.RUnlock()
	return b.drained[agentID]
}

func (b *Broker) assignmentLoop(ctx context.Context) {
	defer b.wg.Done()
	ticker := time.NewTicker(b.cfg.TickInterval())
	defer ticker.Stop()

	for {
		select {
		case <-b.stopCh:
			return
		case <-ctx.Done():
			return
		case <-ticker.C:
			b.processPendingTasks(ctx)
		}
	}
}

func (b *Broker) processPendingTasks(ctx context.Context) {
	tasks, err := b.store.GetPendingTasks(ctx)
	if err != nil {
		b.logger.Error("failed to get pending tasks", "error", err)
		return
	}

	b.logger.Info("processing pending tasks", "count", len(tasks))
	for _, task := range tasks {
		if err := b.assignTask(ctx, task); err != nil {
			b.logger.Warn("failed to assign task", "task_id", task.ID, "error", err)
		}
	}
}

func (b *Broker) assignTask(ctx context.Context, task *store.Task) error {
	b.logger.Info("attempting assignment", "task_id", task.ID, "capabilities", task.RequiredCapabilities, "owner", task.Owner)

	// Query forge for candidates — all agents if no capabilities required, else by primary capability
	var candidates []forge.Persona
	var err error
	if len(task.RequiredCapabilities) == 0 {
		candidates, err = b.forge.ListPersonas(ctx)
		if err != nil {
			b.logger.Error("forge list personas failed", "error", err)
			return err
		}
		b.logger.Info("no capabilities required, all agents eligible", "count", len(candidates))
	} else {
		primaryCap := task.RequiredCapabilities[0]
		candidates, err = b.forge.GetAgentsByCapability(ctx, primaryCap)
		if err != nil {
			b.logger.Error("forge capability query failed", "error", err)
			return err
		}
		b.logger.Info("capability candidates", "count", len(candidates), "primary_cap", primaryCap)
	}

	// Owner-scoped filtering: if task has an owner, only allow agents owned by that owner
	if task.Owner != "" && b.alexandria != nil && b.cfg.Assignment.OwnerFilterEnabled {
		ownedDevices, err := b.alexandria.GetDevicesByOwner(ctx, task.Owner)
		if err != nil {
			b.logger.Warn("failed to query alexandria for owner devices", "owner", task.Owner, "error", err)
		} else {
			ownedNames := make(map[string]bool)
			for _, d := range ownedDevices {
				ownedNames[d.Name] = true
				ownedNames[d.Identifier] = true
			}
			var filtered []forge.Persona
			for _, c := range candidates {
				if ownedNames[c.Name] || ownedNames[c.Slug] {
					filtered = append(filtered, c)
				}
			}
			candidates = filtered
			b.logger.Info("after owner filter", "count", len(candidates), "owner", task.Owner)
		}
	}

	if len(candidates) == 0 {
		b.logger.Warn("no candidates after filtering", "task_id", task.ID)
		_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
			TaskID: task.ID,
			Event:  "unmatched",
		})
		if b.hermes != nil {
			_ = b.hermes.Publish(hermes.SubjectTaskUnmatched(task.ID.String()), map[string]interface{}{
				"task_id":              task.ID.String(),
				"required_capabilities": task.RequiredCapabilities,
				"owner":                task.Owner,
			})
		}
		return nil
	}

	type scoredV2 struct {
		persona forge.Persona
		result  scoring.ScoringResult
	}
	var scoredCandidates []scoredV2

	for _, c := range candidates {
		if b.IsDrained(c.Name) {
			continue
		}
		state, err := b.warren.GetAgentState(ctx, c.Slug)
		if err != nil {
			b.logger.Warn("failed to get agent state", "agent", c.Name, "error", err)
			continue
		}

		tc := b.buildTaskContext(ctx, c, state, task)
		result := b.scorer.ScoreCandidate(tc)
		if result.Eligible {
			scoredCandidates = append(scoredCandidates, scoredV2{persona: c, result: result})
		}
	}

	if len(scoredCandidates) == 0 {
		return nil
	}

	sort.Slice(scoredCandidates, func(i, j int) bool {
		return scoredCandidates[i].result.TotalScore > scoredCandidates[j].result.TotalScore
	})

	winner := scoredCandidates[0]

	// Wake if sleeping
	state, _ := b.warren.GetAgentState(ctx, winner.persona.Slug)
	if state != nil && state.Status == "sleeping" {
		if err := b.warren.WakeAgent(ctx, winner.persona.Name); err != nil {
			b.logger.Warn("failed to wake agent", "agent", winner.persona.Name, "error", err)
			return nil
		}
		time.Sleep(2 * time.Second)
	}

	now := time.Now()
	task.Status = store.StatusAssigned
	task.AssignedAgent = winner.persona.Slug
	task.AssignedAt = &now

	// Apply v2 scoring fields to task for persistence
	b.applyScoring(task, winner.result)

	if err := b.store.UpdateTask(ctx, task); err != nil {
		return err
	}

	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "assigned",
		AgentID: winner.persona.Name,
	})

	if b.hermes != nil {
		_ = b.hermes.Publish(hermes.SubjectTaskAssigned(task.ID.String()), task)
		_ = b.hermes.Publish(hermes.SubjectDispatchAssigned(task.ID.String()), hermes.DispatchAssignedEvent{
			TaskID:         task.ID.String(),
			AssignedAgent:  winner.persona.Slug,
			TotalScore:     winner.result.TotalScore,
			Factors:        winner.result.Factors,
			OversightLevel: winner.result.OversightLevel,
			FastPath:       winner.result.FastPath,
		})
		if winner.result.OversightLevel != "" {
			_ = b.hermes.Publish(hermes.SubjectDispatchOversight(task.ID.String()), hermes.OversightSetEvent{
				TaskID:         task.ID.String(),
				OversightLevel: winner.result.OversightLevel,
			})
		}
	}

	b.logger.Info("task assigned", "task_id", task.ID, "assigned_agent", winner.persona.Name,
		"score", winner.result.TotalScore, "oversight", winner.result.OversightLevel, "fast_path", winner.result.FastPath)
	return nil
}

func (b *Broker) HandleAgentStopped(ctx context.Context, agentID string) {
	tasks, err := b.store.GetActiveTasksForAgent(ctx, agentID)
	if err != nil {
		b.logger.Error("failed to get tasks for stopped agent", "agent", agentID, "error", err)
		return
	}
	for _, task := range tasks {
		task.Status = store.StatusPending
		task.AssignedAgent = ""
		task.AssignedAt = nil
		task.StartedAt = nil
		if err := b.store.UpdateTask(ctx, task); err != nil {
			b.logger.Error("failed to reset task", "task_id", task.ID, "error", err)
			continue
		}
		if b.hermes != nil {
			_ = b.hermes.Publish(hermes.SubjectTaskReassigned(task.ID.String()), map[string]interface{}{
				"task_id": task.ID.String(),
				"reason":  "agent_stopped",
				"agent":   agentID,
			})
		}
	}
}

// SetupSubscriptions registers NATS subscriptions for bookkeeping events.
func (b *Broker) SetupSubscriptions() {
	if b.hermes == nil {
		return
	}

	// Task requests via NATS
	_ = b.hermes.Subscribe(hermes.SubjectTaskRequest, func(_ string, data []byte) {
		var req hermes.TaskRequestEvent
		if err := json.Unmarshal(data, &req); err != nil {
			b.logger.Warn("invalid task request event", "error", err)
			return
		}
		task := &store.Task{
			Owner:                req.Owner,
			Title:                req.Title,
			Description:          req.Description,
			RequiredCapabilities: req.RequiredCapabilities,
			Priority:             req.Priority,
			Status:               store.StatusPending,
			Metadata:             req.Metadata,
			TimeoutSeconds:       req.TimeoutSeconds,
			MaxRetries:           req.MaxRetries,
			Source:               req.Source,
			RetryEligible:        true,
		}
		if task.Priority < 0 {
			task.Priority = 0
		}
		if task.TimeoutSeconds == 0 {
			task.TimeoutSeconds = 300
		}
		if task.MaxRetries == 0 {
			task.MaxRetries = 3
		}
		if task.Source == "" {
			task.Source = "manual"
		}
		if task.Owner == "" {
			task.Owner = "system"
		}
		if err := b.store.CreateTask(context.Background(), task); err != nil {
			b.logger.Error("failed to create task from NATS request", "error", err)
		} else {
			b.logger.Info("task created from NATS request", "task_id", task.ID, "capabilities", task.RequiredCapabilities)
		}
	})

	// Completed events
	_ = b.hermes.Subscribe("swarm.task.*.completed", func(_ string, data []byte) {
		var evt hermes.TaskCompletedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		b.handleCompleted(evt)
	})

	// Failed events
	_ = b.hermes.Subscribe("swarm.task.*.failed", func(_ string, data []byte) {
		var evt hermes.TaskFailedEvent
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		b.handleFailed(evt)
	})

	// Started events (agent acknowledges assignment)
	_ = b.hermes.Subscribe("swarm.task.*.started", func(_ string, data []byte) {
		var evt map[string]interface{}
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		b.handleStarted(evt)
	})

	// Progress events
	_ = b.hermes.Subscribe("swarm.task.*.progress", func(_ string, data []byte) {
		var evt map[string]interface{}
		if err := json.Unmarshal(data, &evt); err != nil {
			return
		}
		b.handleProgress(evt)
	})

	// Agent stopped
	_ = b.hermes.Subscribe(hermes.SubjectAgentStopped, func(subject string, _ []byte) {
		parts := splitSubject(subject)
		if len(parts) >= 3 {
			agentID := parts[2]
			b.logger.Info("agent stopped, reassigning tasks", "agent", agentID)
			b.HandleAgentStopped(context.Background(), agentID)
		}
	})
}

func (b *Broker) handleCompleted(evt hermes.TaskCompletedEvent) {
	ctx := context.Background()
	id, err := uuid.Parse(evt.TaskID)
	if err != nil {
		return
	}
	task, err := b.store.GetTask(ctx, id)
	if err != nil || task == nil {
		return
	}
	now := time.Now()
	task.Status = store.StatusCompleted
	task.Result = evt.Result
	task.CompletedAt = &now
	_ = b.store.UpdateTask(ctx, task)
	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID: task.ID,
		Event:  "completed",
	})

	// Record agent task history for v2 scoring enrichment
	if task.AssignedAgent != "" {
		h := &store.AgentTaskHistory{
			AgentSlug:   task.AssignedAgent,
			TaskID:      task.ID,
			StartedAt:   task.StartedAt,
			CompletedAt: &now,
			Success:     boolPtr(true),
		}
		if task.AssignedAt != nil {
			dur := now.Sub(*task.AssignedAt).Seconds()
			h.DurationSeconds = &dur
		}
		// Extract tokens/cost from result if available
		if evt.Result != nil {
			if tokens, ok := evt.Result["tokens_used"].(float64); ok {
				t := int64(tokens)
				h.TokensUsed = &t
			}
			if cost, ok := evt.Result["cost_usd"].(float64); ok {
				h.CostUSD = &cost
			}
		}
		_ = b.store.CreateAgentTaskHistory(ctx, h)
	}

	// Publish dispatch completed event
	if b.hermes != nil {
		var dur float64
		if task.AssignedAt != nil {
			dur = now.Sub(*task.AssignedAt).Seconds()
		}
		_ = b.hermes.Publish(hermes.SubjectDispatchCompleted(task.ID.String()), hermes.DispatchCompletedEvent{
			TaskID:          task.ID.String(),
			Agent:           task.AssignedAgent,
			DurationSeconds: dur,
		})
	}
}

func (b *Broker) handleFailed(evt hermes.TaskFailedEvent) {
	ctx := context.Background()
	id, err := uuid.Parse(evt.TaskID)
	if err != nil {
		return
	}
	task, err := b.store.GetTask(ctx, id)
	if err != nil || task == nil {
		return
	}
	task.Status = store.StatusFailed
	task.Error = evt.Error
	task.RetryEligible = evt.RetryEligible
	_ = b.store.UpdateTask(ctx, task)
	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID: task.ID,
		Event:  "failed",
	})

	// If retry eligible and retries remain, transition back to pending
	if task.RetryEligible && task.RetryCount < task.MaxRetries {
		task.RetryCount++
		task.Status = store.StatusPending
		task.AssignedAgent = ""
		task.AssignedAt = nil
		task.StartedAt = nil
		task.Error = ""
		_ = b.store.UpdateTask(ctx, task)
		if b.hermes != nil {
			_ = b.hermes.Publish(hermes.SubjectTaskRetry(task.ID.String()), map[string]interface{}{
				"task_id":        task.ID.String(),
				"retry_count":    task.RetryCount,
				"max_retries":    task.MaxRetries,
				"previous_state": "failed",
			})
		}
	} else if !task.RetryEligible || task.RetryCount >= task.MaxRetries {
		// DLQ
		now := time.Now()
		task.CompletedAt = &now
		_ = b.store.UpdateTask(ctx, task)
		if b.hermes != nil {
			_ = b.hermes.Publish(hermes.SubjectTaskDLQ(task.ID.String()), map[string]interface{}{
				"task_id":     task.ID.String(),
				"reason":      "execution_failed",
				"retry_count": task.RetryCount,
				"max_retries": task.MaxRetries,
			})
		}
	}
}

func (b *Broker) handleStarted(evt map[string]interface{}) {
	ctx := context.Background()
	taskID, ok := evt["task_id"].(string)
	if !ok {
		return
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return
	}
	task, err := b.store.GetTask(ctx, id)
	if err != nil || task == nil {
		return
	}
	if task.Status == store.StatusAssigned {
		now := time.Now()
		task.Status = store.StatusInProgress
		task.StartedAt = &now
		_ = b.store.UpdateTask(ctx, task)
	}
	agentID, _ := evt["agent"].(string)
	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "started",
		AgentID: agentID,
	})
}

func (b *Broker) handleProgress(evt map[string]interface{}) {
	ctx := context.Background()
	taskID, ok := evt["task_id"].(string)
	if !ok {
		return
	}
	id, err := uuid.Parse(taskID)
	if err != nil {
		return
	}
	task, err := b.store.GetTask(ctx, id)
	if err != nil || task == nil {
		return
	}
	if task.Status == store.StatusAssigned {
		now := time.Now()
		task.Status = store.StatusInProgress
		task.StartedAt = &now
		_ = b.store.UpdateTask(ctx, task)
	}
	agentID, _ := evt["agent_id"].(string)
	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "progress",
		AgentID: agentID,
		Payload: evt,
	})
}

// buildTaskContext creates a TaskContext for v2 scoring, with optional enrichment.
func (b *Broker) buildTaskContext(ctx context.Context, persona forge.Persona, state *warren.AgentState, task *store.Task) *scoring.TaskContext {
	active, _ := b.store.GetActiveTasksForAgent(ctx, persona.Slug)

	tc := &scoring.TaskContext{
		Task:            task,
		Persona:         persona,
		AgentState:      state,
		ActiveTaskCount: len(active),
		MaxConcurrent:   b.cfg.Assignment.MaxConcurrentPerAgent,
	}

	// Enrich from task metadata
	b.enrichFromMetadata(tc)

	// Enrich from agent history (best-effort)
	b.enrichFromHistory(ctx, tc)

	return tc
}

// enrichFromMetadata extracts scoring hints from task.Metadata.
func (b *Broker) enrichFromMetadata(tc *scoring.TaskContext) {
	m := tc.Task.Metadata
	if m == nil {
		return
	}
	if v, ok := m["risk"].(float64); ok && tc.Task.RiskScore == nil {
		tc.Task.RiskScore = &v
	}
	if v, ok := m["complexity"].(float64); ok && tc.Task.ComplexityScore == nil {
		tc.Task.ComplexityScore = &v
	}
	if v, ok := m["verifiability"].(float64); ok && tc.Task.VerifiabilityScore == nil {
		tc.Task.VerifiabilityScore = &v
	}
	if v, ok := m["reversibility"].(float64); ok && tc.Task.ReversibilityScore == nil {
		tc.Task.ReversibilityScore = &v
	}
	if v, ok := m["uncertainty"].(float64); ok && tc.Task.UncertaintyScore == nil {
		tc.Task.UncertaintyScore = &v
	}
	if v, ok := m["contextuality"].(float64); ok && tc.Task.ContextualityScore == nil {
		tc.Task.ContextualityScore = &v
	}
	if v, ok := m["subjectivity"].(float64); ok && tc.Task.SubjectivityScore == nil {
		tc.Task.SubjectivityScore = &v
	}
	if v, ok := m["duration_class"].(string); ok && tc.Task.DurationClass == "" {
		tc.Task.DurationClass = v
	}
	if v, ok := m["trust_level"].(float64); ok {
		tc.AgentTrustLevel = &v
	}
}

// enrichFromHistory queries agent history for average duration, cost, and trust.
func (b *Broker) enrichFromHistory(ctx context.Context, tc *scoring.TaskContext) {
	avgDur, err := b.store.GetAgentAvgDuration(ctx, tc.Persona.Slug)
	if err == nil && avgDur != nil {
		tc.AgentAvgDuration = avgDur
	}
	avgCost, err := b.store.GetAgentAvgCost(ctx, tc.Persona.Slug)
	if err == nil && avgCost != nil {
		tc.AgentAvgCost = avgCost
	}

	// Trust score from agent_trust table (only if not already set from metadata)
	if tc.AgentTrustLevel == nil {
		category, _ := tc.Task.Metadata["category"].(string)
		severity, _ := tc.Task.Metadata["severity"].(string)
		trust, err := b.store.GetTrustScore(ctx, tc.Persona.Slug, category, severity)
		if err == nil && trust > 0 {
			tc.AgentTrustLevel = &trust
		}
	}
}

// applyScoring writes the ScoringResult fields onto the Task struct for persistence.
func (b *Broker) applyScoring(task *store.Task, result scoring.ScoringResult) {
	task.ScoringVersion = 2
	task.OversightLevel = result.OversightLevel
	task.FastPath = result.FastPath

	// Store factor breakdown as JSON
	factors := make(map[string]interface{})
	for _, f := range result.Factors {
		factors[f.Name] = map[string]interface{}{
			"score":     f.Score,
			"weight":    f.Weight,
			"weighted":  f.Weighted,
			"available": f.Available,
			"reason":    f.Reason,
		}
	}
	factors["total_score"] = result.TotalScore
	task.ScoringFactors = factors
}

func boolPtr(b bool) *bool { return &b }

func splitSubject(subject string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(subject); i++ {
		if subject[i] == '.' {
			parts = append(parts, subject[start:i])
			start = i + 1
		}
	}
	parts = append(parts, subject[start:])
	return parts
}
