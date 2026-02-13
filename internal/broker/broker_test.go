package broker

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/DarlingtonDeveloper/Dispatch/internal/alexandria"
	"github.com/DarlingtonDeveloper/Dispatch/internal/config"
	"github.com/DarlingtonDeveloper/Dispatch/internal/forge"
	"github.com/DarlingtonDeveloper/Dispatch/internal/hermes"
	"github.com/DarlingtonDeveloper/Dispatch/internal/store"
	"github.com/DarlingtonDeveloper/Dispatch/internal/warren"
)

// Mock implementations

type mockStore struct {
	tasks  map[uuid.UUID]*store.Task
	events []*store.TaskEvent
}

func newMockStore() *mockStore {
	return &mockStore{tasks: make(map[uuid.UUID]*store.Task)}
}

func (m *mockStore) CreateTask(_ context.Context, t *store.Task) error {
	t.ID = uuid.New()
	t.CreatedAt = time.Now()
	t.UpdatedAt = time.Now()
	m.tasks[t.ID] = t
	return nil
}
func (m *mockStore) GetTask(_ context.Context, id uuid.UUID) (*store.Task, error) {
	return m.tasks[id], nil
}
func (m *mockStore) ListTasks(_ context.Context, _ store.TaskFilter) ([]*store.Task, error) {
	var out []*store.Task
	for _, t := range m.tasks {
		out = append(out, t)
	}
	return out, nil
}
func (m *mockStore) UpdateTask(_ context.Context, t *store.Task) error {
	m.tasks[t.ID] = t
	return nil
}
func (m *mockStore) GetPendingTasks(_ context.Context) ([]*store.Task, error) {
	var out []*store.Task
	for _, t := range m.tasks {
		if t.Status == store.StatusPending {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *mockStore) GetActiveTasksForAgent(_ context.Context, agentID string) ([]*store.Task, error) {
	var out []*store.Task
	for _, t := range m.tasks {
		if t.AssignedAgent == agentID && (t.Status == store.StatusInProgress || t.Status == store.StatusAssigned) {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *mockStore) GetActiveTasks(_ context.Context) ([]*store.Task, error) {
	var out []*store.Task
	for _, t := range m.tasks {
		if t.Status == store.StatusInProgress || t.Status == store.StatusAssigned {
			out = append(out, t)
		}
	}
	return out, nil
}
func (m *mockStore) CreateTaskEvent(_ context.Context, e *store.TaskEvent) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	m.events = append(m.events, e)
	return nil
}
func (m *mockStore) GetTaskEvents(_ context.Context, taskID uuid.UUID) ([]*store.TaskEvent, error) {
	var out []*store.TaskEvent
	for _, e := range m.events {
		if e.TaskID == taskID {
			out = append(out, e)
		}
	}
	return out, nil
}
func (m *mockStore) GetStats(_ context.Context) (*store.TaskStats, error) {
	return &store.TaskStats{}, nil
}
func (m *mockStore) Close() error { return nil }

type mockHermes struct {
	published []struct {
		subject string
		data    interface{}
	}
}

func (m *mockHermes) Publish(subject string, data interface{}) error {
	m.published = append(m.published, struct {
		subject string
		data    interface{}
	}{subject, data})
	return nil
}
func (m *mockHermes) Subscribe(_ string, _ func(string, []byte)) error { return nil }
func (m *mockHermes) Close()                                           {}

type mockWarren struct {
	states map[string]*warren.AgentState
}

func (m *mockWarren) GetAgentState(_ context.Context, id string) (*warren.AgentState, error) {
	if s, ok := m.states[id]; ok {
		return s, nil
	}
	return &warren.AgentState{Name: id, Status: "stopped"}, nil
}
func (m *mockWarren) WakeAgent(_ context.Context, _ string) error { return nil }
func (m *mockWarren) ListAgents(_ context.Context) ([]warren.AgentState, error) {
	var out []warren.AgentState
	for _, s := range m.states {
		out = append(out, *s)
	}
	return out, nil
}

type mockForge struct {
	personas []forge.Persona
}

func (m *mockForge) ListPersonas(_ context.Context) ([]forge.Persona, error) {
	return m.personas, nil
}
func (m *mockForge) GetAgentsByCapability(_ context.Context, scope string) ([]forge.Persona, error) {
	var out []forge.Persona
	for _, p := range m.personas {
		for _, c := range p.Capabilities {
			if c == scope {
				out = append(out, p)
				break
			}
		}
	}
	return out, nil
}

type mockAlexandria struct {
	devices []alexandria.Device
}

func (m *mockAlexandria) ListDevices(_ context.Context) ([]alexandria.Device, error) {
	return m.devices, nil
}
func (m *mockAlexandria) GetDevicesByOwner(_ context.Context, ownerID string) ([]alexandria.Device, error) {
	var out []alexandria.Device
	for _, d := range m.devices {
		if d.OwnerID == ownerID {
			out = append(out, d)
		}
	}
	return out, nil
}

func testConfig() *config.Config {
	return &config.Config{
		Assignment: config.AssignmentConfig{
			TickIntervalMs:        100,
			WakeTimeoutMs:         1000,
			DefaultTimeoutMs:      5000,
			MaxConcurrentPerAgent: 3,
		},
	}
}

func TestCapabilityMatch(t *testing.T) {
	p := forge.Persona{Name: "lily", Capabilities: []string{"research", "analysis"}}

	if score := CapabilityMatch(p, []string{"research"}); score != 1.0 {
		t.Errorf("expected 1.0, got %f", score)
	}
	if score := CapabilityMatch(p, []string{"code"}); score != 0 {
		t.Errorf("expected 0, got %f", score)
	}
	if score := CapabilityMatch(p, []string{"research", "analysis"}); score != 1.0 {
		t.Errorf("expected 1.0 for multi-cap match, got %f", score)
	}
	if score := CapabilityMatch(p, []string{"research", "code"}); score != 0 {
		t.Errorf("expected 0 for partial multi-cap match, got %f", score)
	}
	if score := CapabilityMatch(p, nil); score != 1.0 {
		t.Errorf("expected 1.0 for empty requirements, got %f", score)
	}
}

func TestScoreCandidate(t *testing.T) {
	s := newMockStore()
	ctx := context.Background()
	p := forge.Persona{Name: "lily", Capabilities: []string{"research"}}
	task := &store.Task{RequiredCapabilities: []string{"research"}, Priority: 10}

	tests := []struct {
		name   string
		status string
		policy string
		want   float64
	}{
		{"always-on ready", "ready", "always-on", 1.0 * 1.0 * 1.0 * 1.5},
		{"on-demand ready", "ready", "on-demand", 1.0 * 1.0 * 0.9 * 1.5},
		{"on-demand sleeping", "sleeping", "on-demand", 1.0 * 0.8 * 0.6 * 1.5},
		{"degraded agent", "degraded", "always-on", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			state := &warren.AgentState{Name: "lily", Status: tt.status, Policy: tt.policy}
			got := ScoreCandidate(p, state, task, s, ctx, 3)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func TestPolicyMultiplier(t *testing.T) {
	tests := []struct {
		name  string
		state *warren.AgentState
		want  float64
	}{
		{"always-on ready", &warren.AgentState{Policy: "always-on", Status: "ready"}, 1.0},
		{"on-demand ready", &warren.AgentState{Policy: "on-demand", Status: "ready"}, 0.9},
		{"on-demand sleeping", &warren.AgentState{Policy: "on-demand", Status: "sleeping"}, 0.6},
		{"on-demand busy", &warren.AgentState{Policy: "on-demand", Status: "busy"}, 0.9},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := PolicyMultiplier(tt.state)
			if math.Abs(got-tt.want) > 0.001 {
				t.Errorf("got %f, want %f", got, tt.want)
			}
		})
	}
}

func TestBrokerAssignment(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
		"nova": {Name: "nova", Status: "sleeping", Policy: "on-demand"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research", "analysis"}},
		{Name: "nova", Slug: "nova", Capabilities: []string{"research", "code"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "test task",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       5,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned, got %s", updated.Status)
	}
	if updated.AssignedAgent != "lily" {
		t.Errorf("expected lily (ready+always-on), got %s", updated.AssignedAgent)
	}
}

func TestOwnerScopedFiltering(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	ownerID := uuid.New().String()
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
		"nova": {Name: "nova", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
		{Name: "nova", Slug: "nova", Capabilities: []string{"research"}},
	}}
	ma := &mockAlexandria{devices: []alexandria.Device{
		{ID: "d1", Name: "nova", OwnerID: ownerID},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                ownerID,
		Title:                "owner-scoped task",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       5,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned, got %s", updated.Status)
	}
	// Should be nova (only agent owned by this owner)
	if updated.AssignedAgent != "nova" {
		t.Errorf("expected nova (owner-scoped), got %s", updated.AssignedAgent)
	}
}

func TestOwnerScopedUnmatched(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	ownerID := uuid.New().String()
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}
	// No devices owned by ownerID
	ma := &mockAlexandria{devices: []alexandria.Device{
		{ID: "d1", Name: "lily", OwnerID: "different-owner"},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                ownerID,
		Title:                "unmatched task",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       5,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending (unmatched), got %s", updated.Status)
	}

	// Should have published unmatched event
	found := false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".unmatched" {
			found = true
		}
	}
	if !found {
		t.Error("expected unmatched event to be published")
	}
}

func TestBrokerDrain(t *testing.T) {
	b := New(nil, nil, nil, nil, nil, testConfig(), discardLogger())
	b.DrainAgent("lily")
	if !b.IsDrained("lily") {
		t.Error("expected lily to be drained")
	}
	b.UndrainAgent("lily")
	if b.IsDrained("lily") {
		t.Error("expected lily to be undrained")
	}
}

func TestTimeoutRetry(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	cfg := testConfig()
	b := New(ms, mh, nil, nil, nil, cfg, discardLogger())

	ctx := context.Background()
	past := time.Now().Add(-10 * time.Second)
	task := &store.Task{
		Owner:                "system",
		Title:                "timeout test",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusInProgress,
		AssignedAgent:        "lily",
		TimeoutSeconds:       1,
		MaxRetries:           2,
		RetryCount:           0,
		AssignedAt:           &past,
		StartedAt:            &past,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.checkTimeouts(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending (retry), got %s", updated.Status)
	}
	if updated.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", updated.RetryCount)
	}
}

func TestTimeoutExhausted(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	cfg := testConfig()
	b := New(ms, mh, nil, nil, nil, cfg, discardLogger())

	ctx := context.Background()
	past := time.Now().Add(-10 * time.Second)
	task := &store.Task{
		Owner:                "system",
		Title:                "timeout exhausted",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusInProgress,
		AssignedAgent:        "lily",
		TimeoutSeconds:       1,
		MaxRetries:           1,
		RetryCount:           1,
		AssignedAt:           &past,
		StartedAt:            &past,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.checkTimeouts(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusTimedOut {
		t.Errorf("expected timed_out, got %s", updated.Status)
	}
}

func TestHandleAgentStopped(t *testing.T) {
	ms := newMockStore()
	cfg := testConfig()
	b := New(ms, &mockHermes{}, nil, nil, nil, cfg, discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:                "system",
		Title:                "running task",
		RequiredCapabilities: []string{"code"},
		Priority:             5,
		Status:               store.StatusInProgress,
		AssignedAgent:        "lily",
		AssignedAt:           &now,
		StartedAt:            &now,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.HandleAgentStopped(ctx, "lily")

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending, got %s", updated.Status)
	}
	if updated.AssignedAgent != "" {
		t.Errorf("expected empty assigned_agent, got %s", updated.AssignedAgent)
	}
}

// --- State machine transition tests ---

func TestHandleStartedTransition(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:                "system",
		Title:                "started test",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusAssigned,
		AssignedAgent:        "scout",
		AssignedAt:           &now,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.handleStarted(map[string]interface{}{
		"task_id": task.ID.String(),
		"agent":   "scout",
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusInProgress {
		t.Errorf("expected in_progress, got %s", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Error("expected started_at to be set")
	}
	// Verify event was recorded
	found := false
	for _, e := range ms.events {
		if e.TaskID == task.ID && e.Event == "started" {
			found = true
		}
	}
	if !found {
		t.Error("expected 'started' event to be recorded")
	}
}

func TestHandleStartedIgnoresNonAssigned(t *testing.T) {
	ms := newMockStore()
	b := New(ms, &mockHermes{}, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:         "system",
		Title:         "already running",
		Status:        store.StatusInProgress,
		AssignedAgent: "scout",
		AssignedAt:    &now,
		StartedAt:     &now,
		Source:        "manual",
	}
	_ = ms.CreateTask(ctx, task)

	b.handleStarted(map[string]interface{}{
		"task_id": task.ID.String(),
		"agent":   "scout",
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusInProgress {
		t.Errorf("expected status unchanged at in_progress, got %s", updated.Status)
	}
}

func TestHandleFailedWithRetry(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:                "system",
		Title:                "retry me",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusInProgress,
		AssignedAgent:        "scout",
		AssignedAt:           &now,
		StartedAt:            &now,
		MaxRetries:           3,
		RetryCount:           0,
		RetryEligible:        true,
		Source:               "manual",
	}
	_ = ms.CreateTask(ctx, task)

	b.handleFailed(hermes.TaskFailedEvent{
		TaskID:        task.ID.String(),
		Error:         "transient error",
		RetryEligible: true,
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending (retried), got %s", updated.Status)
	}
	if updated.RetryCount != 1 {
		t.Errorf("expected retry_count 1, got %d", updated.RetryCount)
	}
	if updated.AssignedAgent != "" {
		t.Errorf("expected assigned_agent cleared, got %s", updated.AssignedAgent)
	}
	if updated.AssignedAt != nil {
		t.Error("expected assigned_at cleared")
	}

	// Verify retry event published
	retryFound := false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".retry" {
			retryFound = true
		}
	}
	if !retryFound {
		t.Error("expected retry event to be published")
	}
}

func TestHandleFailedNotRetryEligible(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:                "system",
		Title:                "permanent failure",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusInProgress,
		AssignedAgent:        "scout",
		AssignedAt:           &now,
		StartedAt:            &now,
		MaxRetries:           3,
		RetryCount:           0,
		RetryEligible:        true,
		Source:               "manual",
	}
	_ = ms.CreateTask(ctx, task)

	b.handleFailed(hermes.TaskFailedEvent{
		TaskID:        task.ID.String(),
		Error:         "invalid input — permanent",
		RetryEligible: false,
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusFailed {
		t.Errorf("expected failed (DLQ), got %s", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Error("expected completed_at set on DLQ")
	}

	// Verify DLQ event published
	dlqFound := false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".dlq" {
			dlqFound = true
		}
	}
	if !dlqFound {
		t.Error("expected DLQ event to be published")
	}
}

func TestHandleFailedRetriesExhausted(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:                "system",
		Title:                "all retries used",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusInProgress,
		AssignedAgent:        "scout",
		AssignedAt:           &now,
		StartedAt:            &now,
		MaxRetries:           2,
		RetryCount:           2,
		RetryEligible:        true,
		Source:               "manual",
	}
	_ = ms.CreateTask(ctx, task)

	b.handleFailed(hermes.TaskFailedEvent{
		TaskID:        task.ID.String(),
		Error:         "failed again",
		RetryEligible: true,
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusFailed {
		t.Errorf("expected failed (exhausted), got %s", updated.Status)
	}

	dlqFound := false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".dlq" {
			dlqFound = true
		}
	}
	if !dlqFound {
		t.Error("expected DLQ event when retries exhausted")
	}
}

func TestTimeoutRetryPublishesRetryAndTimeoutEvents(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	past := time.Now().Add(-10 * time.Second)
	task := &store.Task{
		Owner:                "system",
		Title:                "timeout events test",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusAssigned,
		AssignedAgent:        "scout",
		TimeoutSeconds:       1,
		MaxRetries:           3,
		RetryCount:           0,
		AssignedAt:           &past,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.checkTimeouts(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending (retry), got %s", updated.Status)
	}

	timeoutFound, retryFound := false, false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".timeout" {
			timeoutFound = true
		}
		if p.subject == "swarm.task."+task.ID.String()+".retry" {
			retryFound = true
		}
	}
	if !timeoutFound {
		t.Error("expected timeout event")
	}
	if !retryFound {
		t.Error("expected retry event")
	}
}

func TestTimeoutExhaustedPublishesDLQ(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	past := time.Now().Add(-10 * time.Second)
	task := &store.Task{
		Owner:                "system",
		Title:                "dlq timeout test",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusInProgress,
		AssignedAgent:        "scout",
		TimeoutSeconds:       1,
		MaxRetries:           1,
		RetryCount:           1,
		AssignedAt:           &past,
		StartedAt:            &past,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.checkTimeouts(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusTimedOut {
		t.Errorf("expected timed_out, got %s", updated.Status)
	}
	if updated.CompletedAt == nil {
		t.Error("expected completed_at set on exhausted timeout")
	}

	timeoutFound, dlqFound := false, false
	for _, p := range mh.published {
		if p.subject == "swarm.task."+task.ID.String()+".timeout" {
			timeoutFound = true
		}
		if p.subject == "swarm.task."+task.ID.String()+".dlq" {
			dlqFound = true
		}
	}
	if !timeoutFound {
		t.Error("expected timeout event")
	}
	if !dlqFound {
		t.Error("expected DLQ event on exhausted timeout")
	}
}

func TestTimeoutAssignedState(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	past := time.Now().Add(-10 * time.Second)
	task := &store.Task{
		Owner:                "system",
		Title:                "assigned timeout",
		RequiredCapabilities: []string{"research"},
		Status:               store.StatusAssigned,
		AssignedAgent:        "scout",
		TimeoutSeconds:       1,
		MaxRetries:           2,
		RetryCount:           0,
		AssignedAt:           &past,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.checkTimeouts(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusPending {
		t.Errorf("expected pending (retry from assigned timeout), got %s", updated.Status)
	}
}

func TestCapabilityMatchCaseInsensitive(t *testing.T) {
	p := forge.Persona{Name: "lily", Capabilities: []string{"Research", "ANALYSIS"}}

	if score := CapabilityMatch(p, []string{"research"}); score != 1.0 {
		t.Errorf("expected case-insensitive match, got %f", score)
	}
	if score := CapabilityMatch(p, []string{"analysis"}); score != 1.0 {
		t.Errorf("expected case-insensitive match, got %f", score)
	}
	if score := CapabilityMatch(p, []string{"RESEARCH", "analysis"}); score != 1.0 {
		t.Errorf("expected case-insensitive multi match, got %f", score)
	}
}

func TestScoreCandidatePriorityWeighting(t *testing.T) {
	s := newMockStore()
	ctx := context.Background()
	p := forge.Persona{Name: "lily", Slug: "lily", Capabilities: []string{"research"}}
	state := &warren.AgentState{Name: "lily", Status: "ready", Policy: "always-on"}

	lowPriority := &store.Task{RequiredCapabilities: []string{"research"}, Priority: 0}
	highPriority := &store.Task{RequiredCapabilities: []string{"research"}, Priority: 10}

	lowScore := ScoreCandidate(p, state, lowPriority, s, ctx, 3)
	highScore := ScoreCandidate(p, state, highPriority, s, ctx, 3)

	if highScore <= lowScore {
		t.Errorf("expected high priority (10) to score higher than low (0): high=%f low=%f", highScore, lowScore)
	}
	// priority 0 → weight 1.0, priority 10 → weight 1.5
	if math.Abs(lowScore-1.0) > 0.001 {
		t.Errorf("expected priority 0 score 1.0, got %f", lowScore)
	}
	if math.Abs(highScore-1.5) > 0.001 {
		t.Errorf("expected priority 10 score 1.5, got %f", highScore)
	}
}

func TestHandleProgressTransitionsAssignedToInProgress(t *testing.T) {
	ms := newMockStore()
	b := New(ms, &mockHermes{}, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	task := &store.Task{
		Owner:         "system",
		Title:         "progress test",
		Status:        store.StatusAssigned,
		AssignedAgent: "scout",
		AssignedAt:    &now,
		Source:        "manual",
	}
	_ = ms.CreateTask(ctx, task)

	b.handleProgress(map[string]interface{}{
		"task_id":  task.ID.String(),
		"agent_id": "scout",
		"progress": 0.5,
	})

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusInProgress {
		t.Errorf("expected in_progress after progress event, got %s", updated.Status)
	}
	if updated.StartedAt == nil {
		t.Error("expected started_at set on progress transition")
	}
}

func TestHandleAgentStoppedClearsFields(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	b := New(ms, mh, nil, nil, nil, testConfig(), discardLogger())

	ctx := context.Background()
	now := time.Now()
	// One assigned, one in_progress
	assigned := &store.Task{
		Owner:         "system",
		Title:         "assigned task",
		Status:        store.StatusAssigned,
		AssignedAgent: "lily",
		AssignedAt:    &now,
		Source:        "manual",
	}
	running := &store.Task{
		Owner:         "system",
		Title:         "running task",
		Status:        store.StatusInProgress,
		AssignedAgent: "lily",
		AssignedAt:    &now,
		StartedAt:     &now,
		Source:        "manual",
	}
	_ = ms.CreateTask(ctx, assigned)
	_ = ms.CreateTask(ctx, running)

	b.HandleAgentStopped(ctx, "lily")

	for _, id := range []uuid.UUID{assigned.ID, running.ID} {
		task := ms.tasks[id]
		if task.Status != store.StatusPending {
			t.Errorf("task %s: expected pending, got %s", id, task.Status)
		}
		if task.AssignedAgent != "" {
			t.Errorf("task %s: expected assigned_agent cleared", id)
		}
		if task.AssignedAt != nil {
			t.Errorf("task %s: expected assigned_at cleared", id)
		}
		if task.StartedAt != nil {
			t.Errorf("task %s: expected started_at cleared", id)
		}
	}

	// Should have published reassigned events for both
	reassignCount := 0
	for _, p := range mh.published {
		if p.subject == "swarm.task."+assigned.ID.String()+".reassigned" ||
			p.subject == "swarm.task."+running.ID.String()+".reassigned" {
			reassignCount++
		}
	}
	if reassignCount != 2 {
		t.Errorf("expected 2 reassigned events, got %d", reassignCount)
	}
}

func TestStopIdempotent(t *testing.T) {
	// Reproduces the double-close panic fixed in broker.Stop().
	// Before the fix, calling Stop() twice would panic on close(b.stopCh).
	ms := newMockStore()
	cfg := testConfig()
	b := New(ms, &mockHermes{}, nil, nil, nil, cfg, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)

	// First stop — should close cleanly
	b.Stop()

	// Second stop — must not panic (sync.Once guards the channel close)
	b.Stop()

	cancel() // cleanup
}

func TestStopWithoutStart(t *testing.T) {
	// Stop on a broker that was never started must not hang or panic.
	cfg := testConfig()
	b := New(nil, nil, nil, nil, nil, cfg, discardLogger())

	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK — returned without hanging
	case <-time.After(2 * time.Second):
		t.Fatal("Stop() hung on broker that was never started")
	}
}

func TestStartStopLifecycle(t *testing.T) {
	// Verify Start launches goroutines and Stop terminates them cleanly.
	ms := newMockStore()
	cfg := testConfig()
	b := New(ms, &mockHermes{}, nil, nil, nil, cfg, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	b.Start(ctx)

	// Let the loops run for a couple of ticks
	time.Sleep(300 * time.Millisecond)

	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()

	select {
	case <-done:
		// Clean shutdown
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return within 5 seconds")
	}
}

func TestContextCancelStopsBroker(t *testing.T) {
	// When the parent context is cancelled, the broker loops should exit
	// and Stop() should return promptly.
	ms := newMockStore()
	cfg := testConfig()
	b := New(ms, &mockHermes{}, nil, nil, nil, cfg, discardLogger())

	ctx, cancel := context.WithCancel(context.Background())
	b.Start(ctx)

	// Cancel context — goroutines should exit
	cancel()

	done := make(chan struct{})
	go func() {
		b.Stop()
		close(done)
	}()

	select {
	case <-done:
		// OK
	case <-time.After(5 * time.Second):
		t.Fatal("Stop() did not return after context cancel")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}
