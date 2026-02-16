package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"

	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

// MockStore implements store.Store interface for testing
type MockStore struct {
	mock.Mock
}

func (m *MockStore) GetBacklogItem(ctx context.Context, id uuid.UUID) (*store.BacklogItem, error) {
	args := m.Called(ctx, id)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.BacklogItem), args.Error(1)
}

func (m *MockStore) SubmitEvidence(ctx context.Context, itemID uuid.UUID, stage, criterion, evidence, submittedBy string) error {
	args := m.Called(ctx, itemID, stage, criterion, evidence, submittedBy)
	return args.Error(0)
}

func (m *MockStore) SatisfyCriterion(ctx context.Context, itemID uuid.UUID, stage, criterion, satisfiedBy string) error {
	args := m.Called(ctx, itemID, stage, criterion, satisfiedBy)
	return args.Error(0)
}

func (m *MockStore) GetGateStatus(ctx context.Context, itemID uuid.UUID, stage string) ([]store.GateCriterion, error) {
	args := m.Called(ctx, itemID, stage)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).([]store.GateCriterion), args.Error(1)
}

func (m *MockStore) AllCriteriaMet(ctx context.Context, itemID uuid.UUID, stage string) (bool, error) {
	args := m.Called(ctx, itemID, stage)
	return args.Bool(0), args.Error(1)
}

func (m *MockStore) GetAutonomyConfig(ctx context.Context, tier string) (*store.AutonomyConfig, error) {
	args := m.Called(ctx, tier)
	if args.Get(0) == nil {
		return nil, args.Error(1)
	}
	return args.Get(0).(*store.AutonomyConfig), args.Error(1)
}

func (m *MockStore) UpdateBacklogItem(ctx context.Context, item *store.BacklogItem) error {
	args := m.Called(ctx, item)
	return args.Error(0)
}

func (m *MockStore) IncrementConsecutiveApprovals(ctx context.Context, tier string) (int, error) {
	args := m.Called(ctx, tier)
	return args.Int(0), args.Error(1)
}

func (m *MockStore) UpdateAutonomyConfig(ctx context.Context, tier string, autoApprove bool, consecutiveApprovals, consecutiveCorrections int) error {
	args := m.Called(ctx, tier, autoApprove, consecutiveApprovals, consecutiveCorrections)
	return args.Error(0)
}

func (m *MockStore) ResetAutonomyCounters(ctx context.Context, tier string) error {
	args := m.Called(ctx, tier)
	return args.Error(0)
}

func (m *MockStore) ResetStageToActive(ctx context.Context, itemID uuid.UUID, stage string) error {
	args := m.Called(ctx, itemID, stage)
	return args.Error(0)
}

// Add all other required methods as no-ops for now
func (m *MockStore) CreateTask(ctx context.Context, task *store.Task) error { return nil }
func (m *MockStore) GetTask(ctx context.Context, id uuid.UUID) (*store.Task, error) { return nil, nil }
func (m *MockStore) ListTasks(ctx context.Context, filter store.TaskFilter) ([]*store.Task, error) { return nil, nil }
func (m *MockStore) UpdateTask(ctx context.Context, task *store.Task) error { return nil }
func (m *MockStore) GetPendingTasks(ctx context.Context) ([]*store.Task, error) { return nil, nil }
func (m *MockStore) GetActiveTasksForAgent(ctx context.Context, agentID string) ([]*store.Task, error) { return nil, nil }
func (m *MockStore) GetActiveTasks(ctx context.Context) ([]*store.Task, error) { return nil, nil }
func (m *MockStore) CreateTaskEvent(ctx context.Context, event *store.TaskEvent) error { return nil }
func (m *MockStore) GetTaskEvents(ctx context.Context, taskID uuid.UUID) ([]*store.TaskEvent, error) { return nil, nil }
func (m *MockStore) GetStats(ctx context.Context) (*store.TaskStats, error) { return nil, nil }
func (m *MockStore) CreateAgentTaskHistory(ctx context.Context, h *store.AgentTaskHistory) error { return nil }
func (m *MockStore) GetAgentTaskHistory(ctx context.Context, agentSlug string, limit int) ([]*store.AgentTaskHistory, error) { return nil, nil }
func (m *MockStore) GetAgentAvgDuration(ctx context.Context, agentSlug string) (*float64, error) { return nil, nil }
func (m *MockStore) GetAgentAvgCost(ctx context.Context, agentSlug string) (*float64, error) { return nil, nil }
func (m *MockStore) GetTrustScore(ctx context.Context, agentSlug, category, severity string) (float64, error) { return 0, nil }
func (m *MockStore) CreateBacklogItem(ctx context.Context, item *store.BacklogItem) error { return nil }
func (m *MockStore) ListBacklogItems(ctx context.Context, filter store.BacklogFilter) ([]*store.BacklogItem, error) { return nil, nil }
func (m *MockStore) DeleteBacklogItem(ctx context.Context, id uuid.UUID) error { return nil }
func (m *MockStore) GetNextBacklogItems(ctx context.Context, limit int) ([]*store.BacklogItem, error) { return nil, nil }
func (m *MockStore) CreateDependency(ctx context.Context, dep *store.BacklogDependency) error { return nil }
func (m *MockStore) DeleteDependency(ctx context.Context, id uuid.UUID) error { return nil }
func (m *MockStore) GetDependenciesForItem(ctx context.Context, itemID uuid.UUID) ([]*store.BacklogDependency, error) { return nil, nil }
func (m *MockStore) HasUnresolvedBlockers(ctx context.Context, itemID uuid.UUID) (bool, error) { return false, nil }
func (m *MockStore) ResolveDependenciesForBlocker(ctx context.Context, blockerID uuid.UUID) error { return nil }
func (m *MockStore) CreateOverride(ctx context.Context, o *store.DispatchOverride) error { return nil }
func (m *MockStore) CreateAutonomyEvent(ctx context.Context, e *store.AutonomyEvent) error { return nil }
func (m *MockStore) GetAutonomyMetrics(ctx context.Context, days int) ([]*store.AutonomyMetrics, error) { return nil, nil }
func (m *MockStore) BacklogDiscoveryComplete(ctx context.Context, itemID uuid.UUID, req *store.BacklogDiscoveryCompleteRequest, scoreFn store.ScoreFn, tierFn store.TierFn) (*store.BacklogDiscoveryCompleteResult, error) { return nil, nil }
func (m *MockStore) InitStages(ctx context.Context, itemID uuid.UUID, template []string) error { return nil }
func (m *MockStore) GetCurrentStage(ctx context.Context, itemID uuid.UUID) (string, int, error) { return "", 0, nil }
func (m *MockStore) CreateGateCriteria(ctx context.Context, itemID uuid.UUID, stage string, criteria []string) error { return nil }
func (m *MockStore) SatisfyAllCriteria(ctx context.Context, itemID uuid.UUID, stage string, satisfiedBy string) error { return nil }
func (m *MockStore) GetMedianEstimatedTokens(ctx context.Context) (int64, error) { return 0, nil }
func (m *MockStore) IncrementConsecutiveCorrections(ctx context.Context, tier string) (int, error) { return 0, nil }
func (m *MockStore) Ping(ctx context.Context) error { return nil }
func (m *MockStore) Close() error { return nil }

// MockHermes implements hermes.Client for testing
type MockHermes struct {
	mock.Mock
}

func (m *MockHermes) Publish(subject string, data interface{}) error {
	args := m.Called(subject, data)
	return args.Error(0)
}

func (m *MockHermes) Subscribe(subject string, handler func(string, []byte)) error {
	args := m.Called(subject, handler)
	return args.Error(0)
}

func (m *MockHermes) Close() {
	// No-op for mock
}

func TestSubmitEvidence(t *testing.T) {
	mockStore := &MockStore{}
	mockHermes := &MockHermes{}
	
	handler := &StagesHandler{
		store:  mockStore,
		hermes: mockHermes,
		cfg:    &config.Config{},
	}

	itemID := uuid.New()
	item := &store.BacklogItem{
		ID:        itemID,
		ModelTier: "standard",
	}

	// Test successful evidence submission
	mockStore.On("GetBacklogItem", mock.Anything, itemID).Return(item, nil)
	mockStore.On("SubmitEvidence", mock.Anything, itemID, "implement", "code complete", "Test evidence", "kai").Return(nil)
	mockStore.On("GetGateStatus", mock.Anything, itemID, "implement").Return([]store.GateCriterion{
		{Criterion: "code complete", Satisfied: false},
	}, nil)
	mockStore.On("AllCriteriaMet", mock.Anything, itemID, "implement").Return(false, nil)
	mockHermes.On("Publish", mock.AnythingOfType("string"), mock.Anything).Return(nil)

	reqBody := map[string]string{
		"stage":        "implement",
		"criterion":    "code complete", 
		"evidence":     "Test evidence",
		"submitted_by": "kai",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/backlog/"+itemID.String()+"/gate/evidence", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "kai")

	// Setup router context
	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itemID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handler.SubmitEvidence(rr, req)

	assert.Equal(t, http.StatusCreated, rr.Code)
	mockStore.AssertExpectations(t)
	mockHermes.AssertExpectations(t)
}

func TestAgentCannotSatisfyGate(t *testing.T) {
	mockStore := &MockStore{}
	mockHermes := &MockHermes{}
	cfg := &config.Config{}
	
	// Create a test router like the API tests do
	r := chi.NewRouter()
	stagesHandler := &StagesHandler{
		store:  mockStore,
		hermes: mockHermes,
		cfg:    cfg,
	}
	
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(AgentIDMiddleware)
			r.Post("/backlog/{id}/gate/evidence", stagesHandler.SubmitEvidence)
		})
		r.Group(func(r chi.Router) {
			r.Use(AdminAuthMiddleware("test-admin-token"))
			r.Post("/backlog/{id}/gate/satisfy", stagesHandler.SatisfyGate)
		})
	})

	itemID := uuid.New()
	
	// Test agent trying to satisfy gate (should fail)
	reqBody := map[string]string{
		"criterion":   "code complete",
		"satisfied_by": "agent",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/backlog/"+itemID.String()+"/gate/satisfy", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Agent-ID", "kai")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itemID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// Agent should be unauthorized to satisfy gate
	assert.Equal(t, http.StatusUnauthorized, rr.Code)
}

func TestAdminCanSatisfyGate(t *testing.T) {
	mockStore := &MockStore{}
	mockHermes := &MockHermes{}
	cfg := &config.Config{}
	
	// Create a test router 
	r := chi.NewRouter()
	stagesHandler := &StagesHandler{
		store:  mockStore,
		hermes: mockHermes,
		cfg:    cfg,
	}
	
	r.Route("/api/v1", func(r chi.Router) {
		r.Group(func(r chi.Router) {
			r.Use(AdminAuthMiddleware("test-admin-token"))
			r.Post("/backlog/{id}/gate/satisfy", stagesHandler.SatisfyGate)
		})
	})

	itemID := uuid.New()
	item := &store.BacklogItem{
		ID:           itemID,
		ModelTier:    "standard",
		CurrentStage: "implement",
	}

	// Mock the required store calls
	mockStore.On("GetBacklogItem", mock.Anything, itemID).Return(item, nil)
	mockStore.On("SatisfyCriterion", mock.Anything, itemID, "implement", "code complete", "admin").Return(nil)
	mockStore.On("GetGateStatus", mock.Anything, itemID, "implement").Return([]store.GateCriterion{
		{Criterion: "code complete", Satisfied: true},
	}, nil)
	mockStore.On("AllCriteriaMet", mock.Anything, itemID, "implement").Return(false, nil)
	mockHermes.On("Publish", mock.AnythingOfType("string"), mock.Anything).Return(nil)
	
	reqBody := map[string]string{
		"criterion":   "code complete",
		"satisfied_by": "admin",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/backlog/"+itemID.String()+"/gate/satisfy", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer test-admin-token")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itemID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	r.ServeHTTP(rr, req)

	// Admin should be able to satisfy gate
	assert.Equal(t, http.StatusOK, rr.Code)
	mockStore.AssertExpectations(t)
	mockHermes.AssertExpectations(t)
}

func TestEconomyAutoApproveGraduation(t *testing.T) {
	mockStore := &MockStore{}
	mockHermes := &MockHermes{}
	
	handler := &StagesHandler{
		store:  mockStore,
		hermes: mockHermes,
		cfg:    &config.Config{},
	}

	itemID := uuid.New()
	item := &store.BacklogItem{
		ID:           itemID,
		ModelTier:    "economy",
		CurrentStage: "implement",
	}

	// Test graduation at 20 consecutive approvals
	mockStore.On("GetBacklogItem", mock.Anything, itemID).Return(item, nil)
	mockStore.On("IncrementConsecutiveApprovals", mock.Anything, "economy").Return(20, nil)
	mockStore.On("UpdateAutonomyConfig", mock.Anything, "economy", true, 20, 0).Return(nil)
	mockStore.On("SatisfyCriterion", mock.Anything, itemID, "implement", "code complete", "admin").Return(nil)
	mockStore.On("GetGateStatus", mock.Anything, itemID, "implement").Return([]store.GateCriterion{
		{Criterion: "code complete", Satisfied: true},
	}, nil)
	mockStore.On("AllCriteriaMet", mock.Anything, itemID, "implement").Return(true, nil)
	mockStore.On("UpdateBacklogItem", mock.Anything, mock.AnythingOfType("*store.BacklogItem")).Return(nil)
	
	// Expect both gate satisfied and autonomy graduated events
	mockHermes.On("Publish", mock.AnythingOfType("string"), mock.Anything).Return(nil)

	reqBody := map[string]string{
		"criterion":   "code complete",
		"satisfied_by": "admin",
		"decision":    "approved",
	}
	body, _ := json.Marshal(reqBody)

	req, _ := http.NewRequest("POST", "/api/v1/backlog/"+itemID.String()+"/gate/satisfy", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")

	rctx := chi.NewRouteContext()
	rctx.URLParams.Add("id", itemID.String())
	req = req.WithContext(context.WithValue(req.Context(), chi.RouteCtxKey, rctx))

	rr := httptest.NewRecorder()
	handler.SatisfyGate(rr, req)

	assert.Equal(t, http.StatusOK, rr.Code)
	mockStore.AssertExpectations(t)
	mockHermes.AssertExpectations(t)
}
