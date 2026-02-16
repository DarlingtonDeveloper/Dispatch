package broker

import (
	"context"
	"io"
	"log/slog"
	"math"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/alexandria"
	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/hermes"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

// Mock implementations

type mockStore struct {
	tasks       map[uuid.UUID]*store.Task
	events      []*store.TaskEvent
	trustScores map[string]float64 // key: "slug|category|severity"
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
func (m *mockStore) CreateAgentTaskHistory(_ context.Context, _ *store.AgentTaskHistory) error {
	return nil
}
func (m *mockStore) GetAgentTaskHistory(_ context.Context, _ string, _ int) ([]*store.AgentTaskHistory, error) {
	return nil, nil
}
func (m *mockStore) GetAgentAvgDuration(_ context.Context, _ string) (*float64, error) {
	return nil, nil
}
func (m *mockStore) GetAgentAvgCost(_ context.Context, _ string) (*float64, error) {
	return nil, nil
}
func (m *mockStore) GetTrustScore(_ context.Context, slug, category, severity string) (float64, error) {
	if m.trustScores != nil {
		if v, ok := m.trustScores[slug+"|"+category+"|"+severity]; ok {
			return v, nil
		}
	}
	return 0.0, nil
}
// Backlog interface stubs
func (m *mockStore) CreateBacklogItem(_ context.Context, item *store.BacklogItem) error {
	item.ID = uuid.New()
	return nil
}
func (m *mockStore) GetBacklogItem(_ context.Context, _ uuid.UUID) (*store.BacklogItem, error) {
	return nil, nil
}
func (m *mockStore) ListBacklogItems(_ context.Context, _ store.BacklogFilter) ([]*store.BacklogItem, error) {
	return nil, nil
}
func (m *mockStore) UpdateBacklogItem(_ context.Context, _ *store.BacklogItem) error { return nil }
func (m *mockStore) DeleteBacklogItem(_ context.Context, _ uuid.UUID) error          { return nil }
func (m *mockStore) GetNextBacklogItems(_ context.Context, _ int) ([]*store.BacklogItem, error) {
	return nil, nil
}
func (m *mockStore) CreateDependency(_ context.Context, dep *store.BacklogDependency) error {
	dep.ID = uuid.New()
	return nil
}
func (m *mockStore) DeleteDependency(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockStore) GetDependenciesForItem(_ context.Context, _ uuid.UUID) ([]*store.BacklogDependency, error) {
	return nil, nil
}
func (m *mockStore) HasUnresolvedBlockers(_ context.Context, _ uuid.UUID) (bool, error) {
	return false, nil
}
func (m *mockStore) ResolveDependenciesForBlocker(_ context.Context, _ uuid.UUID) error { return nil }
func (m *mockStore) CreateOverride(_ context.Context, o *store.DispatchOverride) error {
	o.ID = uuid.New()
	return nil
}
func (m *mockStore) CreateAutonomyEvent(_ context.Context, e *store.AutonomyEvent) error {
	e.ID = uuid.New()
	return nil
}
func (m *mockStore) GetAutonomyMetrics(_ context.Context, _ int) ([]*store.AutonomyMetrics, error) {
	return nil, nil
}
func (m *mockStore) BacklogDiscoveryComplete(_ context.Context, _ uuid.UUID, _ *store.BacklogDiscoveryCompleteRequest, _ store.ScoreFn, _ store.TierFn) (*store.BacklogDiscoveryCompleteResult, error) {
	return &store.BacklogDiscoveryCompleteResult{}, nil
}
func (m *mockStore) GetMedianEstimatedTokens(_ context.Context) (int64, error) { return 0, nil }

func (m *mockStore) Ping(_ context.Context) error { return nil }
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
func (m *mockForge) GetModelEffectiveness(_ context.Context) (map[string]forge.ModelTierStats, error) {
	return nil, nil
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
			OwnerFilterEnabled:    true,
		},
		Scoring: config.ScoringConfig{
			Weights: config.ScoringWeights{
				Capability:     0.20,
				Availability:   0.10,
				RiskFit:        0.12,
				CostEfficiency: 0.10,
				Verifiability:  0.08,
				Reversibility:  0.08,
				ComplexityFit:  0.10,
				UncertaintyFit: 0.07,
				DurationFit:    0.05,
				Contextuality:  0.05,
				Subjectivity:   0.05,
			},
			FastPathEnabled: true,
			ParetoEnabled:   false,
		},
		ModelRouting: config.ModelRoutingConfig{
			Enabled:     true,
			DefaultTier: "standard",
			ColdStartRules: []config.ColdStartRule{
				{Name: "config-only", Labels: []string{"config"}, FilePatterns: []string{"*.yaml", "*.yml", "*.toml", "*.json", "*.env"}, Tier: "economy"},
				{Name: "single-file-lint", Labels: []string{"lint", "format"}, MaxFiles: 1, Tier: "economy"},
				{Name: "architecture", Labels: []string{"architecture", "design", "refactor"}, Tier: "premium"},
			},
			Tiers: []config.ModelTierDef{
				{Name: "economy", Models: []string{"claude-haiku-4-5-20251001"}},
				{Name: "standard", Models: []string{"claude-sonnet-4-5-20250929"}},
				{Name: "premium", Models: []string{"claude-opus-4-6"}},
			},
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

// --- E2E: cross-owner assignment ---

// TestCrossOwnerAssignmentE2E exercises the full broker pipeline for cross-owner dispatch.
// When OwnerFilterEnabled=false, a task submitted by owner A should be assignable to agents
// owned by owner B. When OwnerFilterEnabled=true, the same scenario must produce no match.
func TestCrossOwnerAssignmentE2E(t *testing.T) {
	ownerA := uuid.New().String() // task submitter
	ownerB := "different-owner"   // owns all agents

	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily":  {Name: "lily", Status: "ready", Policy: "always-on"},
		"scout": {Name: "scout", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
		{Name: "scout", Slug: "scout", Capabilities: []string{"research", "code"}},
	}}
	// All agents owned by ownerB — ownerA owns nothing
	ma := &mockAlexandria{devices: []alexandria.Device{
		{ID: "d1", Name: "lily", OwnerID: ownerB},
		{ID: "d2", Name: "scout", OwnerID: ownerB},
	}}

	makeTask := func() *store.Task {
		return &store.Task{
			Owner:                ownerA,
			Title:                "cross-owner research",
			RequiredCapabilities: []string{"research"},
			Priority:             5,
			Status:               store.StatusPending,
			TimeoutSeconds:       60,
			Source:               "agent",
			RetryEligible:        true,
		}
	}

	t.Run("filter enabled blocks cross-owner", func(t *testing.T) {
		ms := newMockStore()
		mh := &mockHermes{}
		cfg := testConfig()
		cfg.Assignment.OwnerFilterEnabled = true
		b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

		ctx := context.Background()
		task := makeTask()
		_ = ms.CreateTask(ctx, task)

		b.processPendingTasks(ctx)

		updated := ms.tasks[task.ID]
		if updated.Status != store.StatusPending {
			t.Errorf("expected pending (blocked by owner filter), got %s", updated.Status)
		}
		// Verify unmatched event was published
		unmatchedFound := false
		for _, p := range mh.published {
			if p.subject == "swarm.task."+task.ID.String()+".unmatched" {
				unmatchedFound = true
			}
		}
		if !unmatchedFound {
			t.Error("expected unmatched event when owner filter blocks all candidates")
		}
	})

	t.Run("filter disabled allows cross-owner", func(t *testing.T) {
		ms := newMockStore()
		mh := &mockHermes{}
		cfg := testConfig()
		cfg.Assignment.OwnerFilterEnabled = false
		b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

		ctx := context.Background()
		task := makeTask()
		_ = ms.CreateTask(ctx, task)

		b.processPendingTasks(ctx)

		updated := ms.tasks[task.ID]
		if updated.Status != store.StatusAssigned {
			t.Errorf("expected assigned (owner filter disabled), got %s", updated.Status)
		}
		if updated.AssignedAgent == "" {
			t.Error("expected an agent to be assigned")
		}
		// Verify assigned event was published
		assignedFound := false
		for _, p := range mh.published {
			if p.subject == "swarm.task."+task.ID.String()+".assigned" {
				assignedFound = true
			}
		}
		if !assignedFound {
			t.Error("expected assigned event when cross-owner dispatch succeeds")
		}
	})
}

func TestOwnerFilterDisabled(t *testing.T) {
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
	// Alexandria returns no devices for this owner — with filtering enabled, no candidates would match
	ma := &mockAlexandria{devices: []alexandria.Device{
		{ID: "d1", Name: "lily", OwnerID: "different-owner"},
		{ID: "d2", Name: "nova", OwnerID: "different-owner"},
	}}

	cfg := testConfig()
	cfg.Assignment.OwnerFilterEnabled = false
	b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                ownerID,
		Title:                "cross-owner task",
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
		t.Errorf("expected assigned (owner filter disabled), got %s", updated.Status)
	}
	if updated.AssignedAgent == "" {
		t.Error("expected an agent to be assigned when owner filter is disabled")
	}
}

// --- Nil/empty capabilities assignment tests ---

func TestNilCapabilitiesAssignsToAnyAgent(t *testing.T) {
	// Tasks with nil RequiredCapabilities should be assignable to any agent
	// via ListPersonas(), not silently skipped.
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
		"nova": {Name: "nova", Status: "ready", Policy: "on-demand"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
		{Name: "nova", Slug: "nova", Capabilities: []string{"code"}},
	}}

	cfg := testConfig()
	cfg.Assignment.OwnerFilterEnabled = false
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "no-cap task",
		RequiredCapabilities: nil, // NULL from Postgres
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned for nil capabilities, got %s", updated.Status)
	}
	if updated.AssignedAgent == "" {
		t.Error("expected an agent to be assigned for nil capabilities task")
	}
	// lily should win (always-on > on-demand)
	if updated.AssignedAgent != "lily" {
		t.Errorf("expected lily (always-on ready), got %s", updated.AssignedAgent)
	}
}

func TestEmptyCapabilitiesAssignsToAnyAgent(t *testing.T) {
	// Same as nil but with explicit empty slice.
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"scout": {Name: "scout", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "scout", Slug: "scout", Capabilities: []string{"research", "code"}},
	}}

	cfg := testConfig()
	cfg.Assignment.OwnerFilterEnabled = false
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "empty-cap task",
		RequiredCapabilities: []string{}, // explicit empty
		Priority:             3,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned for empty capabilities, got %s", updated.Status)
	}
	if updated.AssignedAgent != "scout" {
		t.Errorf("expected scout, got %s", updated.AssignedAgent)
	}
}

func TestNilCapabilitiesWithOwnerFilter(t *testing.T) {
	// Nil capabilities + owner filter should still scope to owned agents.
	ms := newMockStore()
	mh := &mockHermes{}
	ownerID := uuid.New().String()
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
		"nova": {Name: "nova", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
		{Name: "nova", Slug: "nova", Capabilities: []string{"code"}},
	}}
	ma := &mockAlexandria{devices: []alexandria.Device{
		{ID: "d1", Name: "nova", OwnerID: ownerID},
	}}

	cfg := testConfig()
	cfg.Assignment.OwnerFilterEnabled = true
	b := New(ms, mh, mw, mf, ma, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                ownerID,
		Title:                "nil-cap owner-scoped",
		RequiredCapabilities: nil,
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned, got %s", updated.Status)
	}
	if updated.AssignedAgent != "nova" {
		t.Errorf("expected nova (owner-scoped), got %s", updated.AssignedAgent)
	}
}

func TestBrokerAssignmentV2Scoring(t *testing.T) {
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
		Title:                "v2 scoring test",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned, got %s", updated.Status)
	}
	// Scoring v2 fields should be populated
	if updated.ScoringVersion != 2 {
		t.Errorf("expected scoring_version=2, got %d", updated.ScoringVersion)
	}
	if updated.ScoringFactors == nil {
		t.Error("expected scoring_factors to be set")
	}
	if updated.OversightLevel == "" {
		t.Error("expected oversight_level to be set")
	}
	// Verify total_score is in the factors
	if _, ok := updated.ScoringFactors["total_score"]; !ok {
		t.Error("expected total_score key in scoring_factors")
	}
}

func TestFastPathAssignment(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	// Task with low complexity, low risk, high reversibility → fast path eligible
	task := &store.Task{
		Owner:                "system",
		Title:                "fast path test",
		RequiredCapabilities: []string{"research"},
		Priority:             1,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Metadata: map[string]interface{}{
			"complexity":    0.1,
			"risk":          0.1,
			"reversibility": 0.9,
		},
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Errorf("expected assigned, got %s", updated.Status)
	}
	if !updated.FastPath {
		t.Error("expected fast_path=true for low-risk simple task")
	}
}

func TestTrustScoreLookup(t *testing.T) {
	ms := newMockStore()
	ms.trustScores = map[string]float64{
		"lily||": 0.85, // default category/severity
	}
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	// Set low risk + high verifiability/reversibility so trust is the deciding factor
	task := &store.Task{
		Owner:                "system",
		Title:                "trust lookup test",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Metadata: map[string]interface{}{
			"risk":          0.1,
			"verifiability": 0.9,
			"reversibility": 0.9,
		},
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	if updated.OversightLevel == "" {
		t.Error("expected oversight_level to be set")
	}
	// oversight = 0.1*0.35 + 0.1*0.25 + 0.1*0.25 + 0.15*0.15 = 0.035+0.025+0.025+0.0225 = 0.1075 → autonomous
	if updated.OversightLevel != "autonomous" {
		t.Errorf("expected autonomous with high trust + low risk, got %s", updated.OversightLevel)
	}
}

func TestTrustScoreLookupByCategorySeverity(t *testing.T) {
	ms := newMockStore()
	ms.trustScores = map[string]float64{
		"lily|deploy|critical": 0.3, // low trust for critical deploys
	}
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"deploy"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "critical deploy",
		RequiredCapabilities: []string{"deploy"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Metadata: map[string]interface{}{
			"category": "deploy",
			"severity": "critical",
		},
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	// Low trust (0.3) should produce higher oversight
	if updated.OversightLevel == "autonomous" {
		t.Error("expected non-autonomous oversight with low trust score")
	}
}

func TestTrustScoreMetadataOverridesDB(t *testing.T) {
	ms := newMockStore()
	ms.trustScores = map[string]float64{
		"lily||": 0.2, // low trust in DB — should be ignored
	}
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "metadata trust override",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Metadata: map[string]interface{}{
			"trust_level":   0.95, // high trust via metadata — takes precedence
			"risk":          0.1,
			"verifiability": 0.9,
			"reversibility": 0.9,
		},
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	// oversight = 0.1*0.35 + 0.1*0.25 + 0.1*0.25 + 0.05*0.15 = 0.035+0.025+0.025+0.0075 = 0.0925 → autonomous
	// Metadata trust (0.95) should take precedence over DB trust (0.2)
	if updated.OversightLevel != "autonomous" {
		t.Errorf("expected autonomous with high metadata trust, got %s", updated.OversightLevel)
	}
}

func TestTrustScoreZeroUsesDefault(t *testing.T) {
	// No trust score in DB → AgentTrustLevel stays nil → default 0.5 used by scorer
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "no trust score",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	// With default trust (0.5) and default risk/verifiability/reversibility (all 0.5),
	// oversight should land in the middle range
	if updated.OversightLevel == "" {
		t.Error("expected oversight_level to be set even without trust score")
	}
}

func TestBrokerAssignmentPopulatesModelRouting(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "model routing test",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	if updated.ModelTier == "" {
		t.Error("expected model_tier to be set after assignment")
	}
	if updated.RecommendedModel == "" {
		t.Error("expected recommended_model to be set after assignment")
	}
	if updated.RoutingMethod != "cold_start" {
		t.Errorf("expected routing_method 'cold_start', got '%s'", updated.RoutingMethod)
	}
	if updated.Runtime == "" {
		t.Error("expected runtime to be set after assignment")
	}
}

func TestBrokerAssignmentModelRoutingWithLabels(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"config"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "config update",
		RequiredCapabilities: []string{"config"},
		Priority:             1,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Labels:               []string{"config"},
		FilePatterns:         []string{"app.yaml"},
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	if updated.ModelTier != "economy" {
		t.Errorf("expected economy tier for config task, got '%s'", updated.ModelTier)
	}
	if updated.Runtime != "picoclaw" {
		t.Errorf("expected picoclaw runtime for economy tier, got '%s'", updated.Runtime)
	}
}

func TestBrokerAssignmentOneWayDoorForcesPremium(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"config"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "irreversible deploy",
		RequiredCapabilities: []string{"config"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
		Labels:               []string{"config"},
		FilePatterns:         []string{"app.yaml"},
		OneWayDoor:           true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusAssigned {
		t.Fatalf("expected assigned, got %s", updated.Status)
	}
	if updated.ModelTier != "premium" {
		t.Errorf("expected premium tier for one_way_door task, got '%s'", updated.ModelTier)
	}
	if updated.Runtime != "openclaw" {
		t.Errorf("expected openclaw runtime for premium tier, got '%s'", updated.Runtime)
	}
}

func TestBrokerAssignmentModelRoutingEvent(t *testing.T) {
	ms := newMockStore()
	mh := &mockHermes{}
	mw := &mockWarren{states: map[string]*warren.AgentState{
		"lily": {Name: "lily", Status: "ready", Policy: "always-on"},
	}}
	mf := &mockForge{personas: []forge.Persona{
		{Name: "lily", Slug: "lily", Capabilities: []string{"research"}},
	}}

	cfg := testConfig()
	b := New(ms, mh, mw, mf, nil, cfg, discardLogger())

	ctx := context.Background()
	task := &store.Task{
		Owner:                "system",
		Title:                "event check",
		RequiredCapabilities: []string{"research"},
		Priority:             5,
		Status:               store.StatusPending,
		TimeoutSeconds:       60,
		Source:               "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(ctx, task)

	b.processPendingTasks(ctx)

	// Find the dispatch assigned event
	var dispatchEvt *hermes.DispatchAssignedEvent
	for _, p := range mh.published {
		if evt, ok := p.data.(hermes.DispatchAssignedEvent); ok {
			dispatchEvt = &evt
			break
		}
	}
	if dispatchEvt == nil {
		t.Fatal("expected DispatchAssignedEvent to be published")
	}
	if dispatchEvt.ModelTier == "" {
		t.Error("expected model_tier in DispatchAssignedEvent")
	}
	if dispatchEvt.RecommendedModel == "" {
		t.Error("expected recommended_model in DispatchAssignedEvent")
	}
	if dispatchEvt.RoutingMethod != "cold_start" {
		t.Errorf("expected routing_method 'cold_start' in event, got '%s'", dispatchEvt.RoutingMethod)
	}
	if dispatchEvt.Runtime == "" {
		t.Error("expected runtime in DispatchAssignedEvent")
	}
}

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// Stage engine stubs (satisfy store.Store interface)
func (m *mockStore) InitStages(_ context.Context, _ uuid.UUID, _ []string) error { return nil }
func (m *mockStore) GetCurrentStage(_ context.Context, _ uuid.UUID) (string, int, error) { return "", 0, nil }
func (m *mockStore) CreateGateCriteria(_ context.Context, _ uuid.UUID, _ string, _ []string) error { return nil }
func (m *mockStore) SatisfyCriterion(_ context.Context, _ uuid.UUID, _, _, _ string) error { return nil }
func (m *mockStore) SatisfyAllCriteria(_ context.Context, _ uuid.UUID, _, _ string) error { return nil }
func (m *mockStore) GetGateStatus(_ context.Context, _ uuid.UUID, _ string) ([]store.GateCriterion, error) { return nil, nil }
func (m *mockStore) AllCriteriaMet(_ context.Context, _ uuid.UUID, _ string) (bool, error) { return true, nil }
