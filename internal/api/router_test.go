package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/MikeSquared-Agency/Dispatch/internal/broker"
	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

// Mocks
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
func (m *mockStore) ListTasks(_ context.Context, f store.TaskFilter) ([]*store.Task, error) {
	var out []*store.Task
	for _, t := range m.tasks {
		if f.Status != nil && t.Status != *f.Status {
			continue
		}
		if f.Owner != "" && t.Owner != f.Owner {
			continue
		}
		if f.Source != "" && t.Source != f.Source {
			continue
		}
		if f.Agent != "" && t.AssignedAgent != f.Agent {
			continue
		}
		out = append(out, t)
	}
	return out, nil
}
func (m *mockStore) UpdateTask(_ context.Context, t *store.Task) error {
	m.tasks[t.ID] = t
	return nil
}
func (m *mockStore) GetPendingTasks(_ context.Context) ([]*store.Task, error)                      { return nil, nil }
func (m *mockStore) GetActiveTasksForAgent(_ context.Context, _ string) ([]*store.Task, error)     { return nil, nil }
func (m *mockStore) GetActiveTasks(_ context.Context) ([]*store.Task, error)                       { return nil, nil }
func (m *mockStore) CreateTaskEvent(_ context.Context, e *store.TaskEvent) error {
	e.ID = uuid.New()
	m.events = append(m.events, e)
	return nil
}
func (m *mockStore) GetTaskEvents(_ context.Context, _ uuid.UUID) ([]*store.TaskEvent, error) {
	return nil, nil
}
func (m *mockStore) GetStats(_ context.Context) (*store.TaskStats, error) {
	return &store.TaskStats{TotalPending: 1}, nil
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
func (m *mockStore) GetTrustScore(_ context.Context, _, _, _ string) (float64, error) {
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

// Stage engine stubs
func (m *mockStore) InitStages(_ context.Context, _ uuid.UUID, _ []string) error         { return nil }
func (m *mockStore) GetCurrentStage(_ context.Context, _ uuid.UUID) (string, int, error)  { return "", 0, nil }
func (m *mockStore) CreateGateCriteria(_ context.Context, _ uuid.UUID, _ string, _ []string) error { return nil }
func (m *mockStore) SatisfyCriterion(_ context.Context, _ uuid.UUID, _, _, _ string) error { return nil }
func (m *mockStore) SatisfyAllCriteria(_ context.Context, _ uuid.UUID, _, _ string) error  { return nil }
func (m *mockStore) GetGateStatus(_ context.Context, _ uuid.UUID, _ string) ([]store.GateCriterion, error) { return nil, nil }
func (m *mockStore) AllCriteriaMet(_ context.Context, _ uuid.UUID, _ string) (bool, error) { return true, nil }

func (m *mockStore) Ping(_ context.Context) error { return nil }
func (m *mockStore) Close() error { return nil }

type mockHermes struct{}

func (m *mockHermes) Publish(_ string, _ interface{}) error            { return nil }
func (m *mockHermes) Subscribe(_ string, _ func(string, []byte)) error { return nil }
func (m *mockHermes) Close()                                           {}

type mockWarren struct{}

func (m *mockWarren) GetAgentState(_ context.Context, id string) (*warren.AgentState, error) {
	return &warren.AgentState{Name: id, Status: "ready", Policy: "always-on"}, nil
}
func (m *mockWarren) WakeAgent(_ context.Context, _ string) error { return nil }
func (m *mockWarren) ListAgents(_ context.Context) ([]warren.AgentState, error) {
	return []warren.AgentState{{Name: "test", Status: "ready", Policy: "always-on"}}, nil
}

type mockForge struct{}

func (m *mockForge) ListPersonas(_ context.Context) ([]forge.Persona, error)                   { return nil, nil }
func (m *mockForge) GetAgentsByCapability(_ context.Context, _ string) ([]forge.Persona, error) { return nil, nil }
func (m *mockForge) GetModelEffectiveness(_ context.Context) (map[string]forge.ModelTierStats, error) {
	return nil, nil
}

func setupTestRouter() (http.Handler, *mockStore) {
	ms := newMockStore()
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg := &config.Config{
		Assignment: config.AssignmentConfig{TickIntervalMs: 100, MaxConcurrentPerAgent: 3},
		Scoring: config.ScoringConfig{
			Weights: config.ScoringWeights{
				Capability: 0.20, Availability: 0.10, RiskFit: 0.12, CostEfficiency: 0.10,
				Verifiability: 0.08, Reversibility: 0.08, ComplexityFit: 0.10,
				UncertaintyFit: 0.07, DurationFit: 0.05, Contextuality: 0.05, Subjectivity: 0.05,
			},
			FastPathEnabled: true,
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
	b := broker.New(ms, &mockHermes{}, &mockWarren{}, &mockForge{}, nil, cfg, logger)
	bs := scoring.NewBacklogScorer(scoring.DefaultBacklogWeights())
	router := NewRouter(ms, &mockHermes{}, &mockWarren{}, &mockForge{}, b, bs, cfg, "test-token", logger)
	return router, ms
}

func TestCreateTask(t *testing.T) {
	router, _ := setupTestRouter()

	body := `{"title":"Test Task","required_capabilities":["research"],"priority":2,"owner":"mike-d"}`
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Errorf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var task store.Task
	_ = json.NewDecoder(w.Body).Decode(&task)
	if task.Title != "Test Task" {
		t.Errorf("expected 'Test Task', got '%s'", task.Title)
	}
	if task.Priority != 2 {
		t.Errorf("expected priority 2, got %d", task.Priority)
	}
	if task.Owner != "mike-d" {
		t.Errorf("expected owner 'mike-d', got '%s'", task.Owner)
	}
}

func TestCreateTaskMissingTitle(t *testing.T) {
	router, _ := setupTestRouter()

	body := `{"description":"No title"}`
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestListTasks(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestMissingAgentID(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/tasks", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestHealthEndpoint(t *testing.T) {
	router := NewMetricsRouter()
	req := httptest.NewRequest("GET", "/health", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestStatsRequiresAdminToken(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestStatsWithToken(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", w.Code)
	}
}

func TestCompleteTask(t *testing.T) {
	router, ms := setupTestRouter()

	task := &store.Task{
		Owner:                "system",
		Title:                "Complete Me",
		RequiredCapabilities: []string{"code"},
		Priority:             5,
		Status:               store.StatusInProgress,
		TimeoutSeconds:       300,
		Source:                "manual",
		RetryEligible:        true,
	}
	_ = ms.CreateTask(context.Background(), task)

	body := `{"result":{"output":"done"}}`
	req := httptest.NewRequest("POST", "/api/v1/tasks/"+task.ID.String()+"/complete", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "worker")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusCompleted {
		t.Errorf("expected completed, got %s", updated.Status)
	}
}

// Add missing autonomy methods to existing mockStore
func (m *mockStore) GetAutonomyConfig(ctx context.Context, tier string) (*store.AutonomyConfig, error) { return nil, nil }
func (m *mockStore) UpdateAutonomyConfig(ctx context.Context, tier string, autoApprove bool, consecutiveApprovals, consecutiveCorrections int) error { return nil }
func (m *mockStore) IncrementConsecutiveApprovals(ctx context.Context, tier string) (int, error) { return 0, nil }
func (m *mockStore) IncrementConsecutiveCorrections(ctx context.Context, tier string) (int, error) { return 0, nil }
func (m *mockStore) ResetAutonomyCounters(ctx context.Context, tier string) error { return nil }
func (m *mockStore) SubmitEvidence(ctx context.Context, itemID uuid.UUID, stage, criterion, evidence, submittedBy string) error { return nil }
func (m *mockStore) ResetStageToActive(ctx context.Context, itemID uuid.UUID, stage string) error { return nil }

