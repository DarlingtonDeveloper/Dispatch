package broker

import (
	"context"
	"encoding/json"
	"log/slog"
	"sort"
	"sync"
	"time"

	"github.com/google/uuid"

	"github.com/DarlingtonDeveloper/Dispatch/internal/alexandria"
	"github.com/DarlingtonDeveloper/Dispatch/internal/config"
	"github.com/DarlingtonDeveloper/Dispatch/internal/forge"
	"github.com/DarlingtonDeveloper/Dispatch/internal/hermes"
	"github.com/DarlingtonDeveloper/Dispatch/internal/store"
	"github.com/DarlingtonDeveloper/Dispatch/internal/warren"
)

type Broker struct {
	store      store.Store
	hermes     hermes.Client
	warren     warren.Client
	forge      forge.Client
	alexandria alexandria.Client
	cfg        *config.Config
	logger     *slog.Logger

	drainedMu sync.RWMutex
	drained   map[string]bool

	stopCh chan struct{}
	wg     sync.WaitGroup
}

func New(s store.Store, h hermes.Client, w warren.Client, f forge.Client, a alexandria.Client, cfg *config.Config, logger *slog.Logger) *Broker {
	return &Broker{
		store:      s,
		hermes:     h,
		warren:     w,
		forge:      f,
		alexandria: a,
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
	close(b.stopCh)
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

	for _, task := range tasks {
		if err := b.assignTask(ctx, task); err != nil {
			b.logger.Warn("failed to assign task", "task_id", task.ID, "error", err)
		}
	}
}

func (b *Broker) assignTask(ctx context.Context, task *store.Task) error {
	candidates, err := b.forge.GetAgentsByCapability(ctx, task.Scope)
	if err != nil {
		return err
	}

	// Owner-scoped filtering: if task has an owner, only allow agents owned by that owner
	if task.Owner != "" && b.alexandria != nil {
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
		}
	}

	if len(candidates) == 0 {
		_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
			TaskID: task.ID,
			Event:  "unmatched",
		})
		if b.hermes != nil {
			_ = b.hermes.Publish(hermes.SubjectTaskUnmatched(task.ID.String()), map[string]interface{}{
				"task_id": task.ID.String(),
				"scope":   task.Scope,
				"owner":   task.Owner,
			})
		}
		return nil
	}

	type scored struct {
		persona forge.Persona
		score   float64
	}
	var scoredCandidates []scored

	for _, c := range candidates {
		if b.IsDrained(c.Name) {
			continue
		}
		state, err := b.warren.GetAgentState(ctx, c.Slug)
		if err != nil {
			b.logger.Warn("failed to get agent state", "agent", c.Name, "error", err)
			continue
		}

		s := ScoreCandidate(c, state, task, b.store, ctx, b.cfg.Assignment.MaxConcurrentPerAgent)
		if s > 0 {
			scoredCandidates = append(scoredCandidates, scored{persona: c, score: s})
		}
	}

	if len(scoredCandidates) == 0 {
		return nil
	}

	sort.Slice(scoredCandidates, func(i, j int) bool {
		return scoredCandidates[i].score > scoredCandidates[j].score
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
	task.Assignee = winner.persona.Slug
	task.AssignedAt = &now

	if err := b.store.UpdateTask(ctx, task); err != nil {
		return err
	}

	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID:  task.ID,
		Event:   "assigned",
		AgentID: winner.persona.Name,
	})

	// Publish full task payload to NATS — no gateway delivery
	if b.hermes != nil {
		_ = b.hermes.Publish(hermes.SubjectTaskAssigned(task.ID.String()), task)
	}

	b.logger.Info("task assigned", "task_id", task.ID, "assignee", winner.persona.Name, "score", winner.score)
	return nil
}

func (b *Broker) HandleAgentStopped(ctx context.Context, agentID string) {
	tasks, err := b.store.GetRunningTasksForAgent(ctx, agentID)
	if err != nil {
		b.logger.Error("failed to get tasks for stopped agent", "agent", agentID, "error", err)
		return
	}
	for _, task := range tasks {
		task.Status = store.StatusPending
		task.Assignee = ""
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
			Requester:   req.Requester,
			Owner:       req.Owner,
			Submitter:   req.Submitter,
			Title:       req.Title,
			Description: req.Description,
			Scope:       req.Scope,
			Priority:    req.Priority,
			Status:      store.StatusPending,
			Context:     req.Context,
			TimeoutMs:   req.TimeoutMs,
			MaxRetries:  req.MaxRetries,
		}
		if task.Priority == 0 {
			task.Priority = 3
		}
		if task.TimeoutMs == 0 {
			task.TimeoutMs = 300000
		}
		if task.MaxRetries == 0 {
			task.MaxRetries = 1
		}
		if err := b.store.CreateTask(context.Background(), task); err != nil {
			b.logger.Error("failed to create task from NATS request", "error", err)
		} else {
			b.logger.Info("task created from NATS request", "task_id", task.ID, "scope", task.Scope)
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
	now := time.Now()
	task.Status = store.StatusFailed
	task.Error = evt.Error
	task.CompletedAt = &now
	_ = b.store.UpdateTask(ctx, task)
	_ = b.store.CreateTaskEvent(ctx, &store.TaskEvent{
		TaskID: task.ID,
		Event:  "failed",
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
		task.Status = store.StatusRunning
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
