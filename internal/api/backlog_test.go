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
	"github.com/MikeSquared-Agency/Dispatch/internal/scoring"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

// backlogMockStore extends mockStore with working backlog storage for API tests.
type backlogMockStore struct {
	mockStore
	backlogItems map[uuid.UUID]*store.BacklogItem
	deps         map[uuid.UUID]*store.BacklogDependency
	overrides    []*store.DispatchOverride
	autoEvents   []*store.AutonomyEvent
}

func newBacklogMockStore() *backlogMockStore {
	return &backlogMockStore{
		mockStore:    mockStore{tasks: make(map[uuid.UUID]*store.Task)},
		backlogItems: make(map[uuid.UUID]*store.BacklogItem),
		deps:         make(map[uuid.UUID]*store.BacklogDependency),
	}
}

func (m *backlogMockStore) CreateBacklogItem(_ context.Context, item *store.BacklogItem) error {
	item.ID = uuid.New()
	item.CreatedAt = time.Now()
	item.UpdatedAt = time.Now()
	m.backlogItems[item.ID] = item
	return nil
}

func (m *backlogMockStore) GetBacklogItem(_ context.Context, id uuid.UUID) (*store.BacklogItem, error) {
	item, ok := m.backlogItems[id]
	if !ok {
		return nil, nil
	}
	return item, nil
}

func (m *backlogMockStore) ListBacklogItems(_ context.Context, filter store.BacklogFilter) ([]*store.BacklogItem, error) {
	var out []*store.BacklogItem
	for _, item := range m.backlogItems {
		if filter.Status != nil && item.Status != *filter.Status {
			continue
		}
		if filter.Domain != "" && item.Domain != filter.Domain {
			continue
		}
		if filter.AssignedTo != "" && item.AssignedTo != filter.AssignedTo {
			continue
		}
		if filter.ItemType != "" && item.ItemType != filter.ItemType {
			continue
		}
		out = append(out, item)
	}
	return out, nil
}

func (m *backlogMockStore) UpdateBacklogItem(_ context.Context, item *store.BacklogItem) error {
	m.backlogItems[item.ID] = item
	return nil
}

func (m *backlogMockStore) GetNextBacklogItems(_ context.Context, limit int) ([]*store.BacklogItem, error) {
	var out []*store.BacklogItem
	for _, item := range m.backlogItems {
		if item.Status == store.BacklogStatusReady {
			out = append(out, item)
			if len(out) >= limit {
				break
			}
		}
	}
	return out, nil
}

func (m *backlogMockStore) CreateDependency(_ context.Context, dep *store.BacklogDependency) error {
	dep.ID = uuid.New()
	dep.CreatedAt = time.Now()
	m.deps[dep.ID] = dep
	return nil
}

func (m *backlogMockStore) DeleteDependency(_ context.Context, id uuid.UUID) error {
	delete(m.deps, id)
	return nil
}

func (m *backlogMockStore) GetDependenciesForItem(_ context.Context, itemID uuid.UUID) ([]*store.BacklogDependency, error) {
	var out []*store.BacklogDependency
	for _, d := range m.deps {
		if d.BlockerID == itemID || d.BlockedID == itemID {
			out = append(out, d)
		}
	}
	return out, nil
}

func (m *backlogMockStore) CreateOverride(_ context.Context, o *store.DispatchOverride) error {
	o.ID = uuid.New()
	o.CreatedAt = time.Now()
	m.overrides = append(m.overrides, o)
	return nil
}

func (m *backlogMockStore) CreateAutonomyEvent(_ context.Context, e *store.AutonomyEvent) error {
	e.ID = uuid.New()
	e.CreatedAt = time.Now()
	m.autoEvents = append(m.autoEvents, e)
	return nil
}

func (m *backlogMockStore) BacklogDiscoveryComplete(_ context.Context, itemID uuid.UUID, req *store.BacklogDiscoveryCompleteRequest, scoreFn store.ScoreFn, tierFn store.TierFn) (*store.BacklogDiscoveryCompleteResult, error) {
	item, ok := m.backlogItems[itemID]
	if !ok {
		return nil, nil
	}
	if req.Impact != nil {
		item.Impact = req.Impact
	}
	if req.Urgency != nil {
		item.Urgency = req.Urgency
	}
	if req.Park {
		item.Status = store.BacklogStatusBacklog
	} else {
		item.Status = store.BacklogStatusPlanned
	}
	item.ScoresSource = "discovery"
	if scoreFn != nil {
		score := scoreFn(item, false, 0)
		item.PriorityScore = &score
	}
	if tierFn != nil {
		item.ModelTier = tierFn(item)
	}
	return &store.BacklogDiscoveryCompleteResult{
		Item:      item,
		ModelTier: item.ModelTier,
	}, nil
}

func setupBacklogTestRouter() (http.Handler, *backlogMockStore) {
	ms := newBacklogMockStore()
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
				{Name: "economy", Models: []string{"anthropic/claude-3-5-haiku-latest"}},
				{Name: "standard", Models: []string{"anthropic/claude-sonnet-4-20250514"}},
				{Name: "premium", Models: []string{"anthropic/claude-opus-4-20250514"}},
			},
		},
	}
	b := broker.New(ms, &mockHermes{}, &mockWarren{}, &mockForge{}, nil, cfg, logger)
	bs := scoring.NewBacklogScorer(scoring.DefaultBacklogWeights())
	router := NewRouter(ms, &mockHermes{}, &mockWarren{}, &mockForge{}, b, bs, cfg, "test-token", logger)
	return router, ms
}

// --- Backlog CRUD Tests ---

func TestCreateBacklogItem(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	body := `{"title":"Build auth system","domain":"infrastructure","impact":0.8,"urgency":0.7,"item_type":"epic"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var item store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&item)
	if item.Title != "Build auth system" {
		t.Errorf("expected title 'Build auth system', got '%s'", item.Title)
	}
	if item.ItemType != "epic" {
		t.Errorf("expected item_type 'epic', got '%s'", item.ItemType)
	}
	if item.Domain != "infrastructure" {
		t.Errorf("expected domain 'infrastructure', got '%s'", item.Domain)
	}
	if item.Status != store.BacklogStatusBacklog {
		t.Errorf("expected status 'backlog', got '%s'", item.Status)
	}
	if item.PriorityScore == nil {
		t.Error("expected priority_score to be set from scoring")
	}
}

func TestCreateBacklogItemMissingTitle(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	body := `{"domain":"infrastructure"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestCreateBacklogItemManualPriorityMapsToUrgency(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	body := `{"title":"Urgent fix","manual_priority":0.95}`
	req := httptest.NewRequest("POST", "/api/v1/backlog", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var item store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&item)

	stored := ms.backlogItems[item.ID]
	if stored.Urgency == nil || *stored.Urgency != 0.95 {
		t.Errorf("expected urgency mapped from manual_priority=0.95, got %v", stored.Urgency)
	}
}

func TestListBacklogItems(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	// Seed items
	for _, title := range []string{"Item A", "Item B", "Item C"} {
		_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{
			Title:    title,
			ItemType: "task",
			Status:   store.BacklogStatusBacklog,
		})
	}

	req := httptest.NewRequest("GET", "/api/v1/backlog", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var items []store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 3 {
		t.Errorf("expected 3 items, got %d", len(items))
	}
}

func TestListBacklogItemsFilterByStatus(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{Title: "Ready", ItemType: "task", Status: store.BacklogStatusReady})
	_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{Title: "Backlog", ItemType: "task", Status: store.BacklogStatusBacklog})

	req := httptest.NewRequest("GET", "/api/v1/backlog?status=ready", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var items []store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 1 {
		t.Errorf("expected 1 ready item, got %d", len(items))
	}
}

func TestGetBacklogItem(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Get Me", ItemType: "task", Status: store.BacklogStatusBacklog, Domain: "product"}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("GET", "/api/v1/backlog/"+item.ID.String(), nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var got store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got.Title != "Get Me" {
		t.Errorf("expected title 'Get Me', got '%s'", got.Title)
	}
}

func TestGetBacklogItemNotFound(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/backlog/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestUpdateBacklogItem(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Update Me", ItemType: "task", Status: store.BacklogStatusBacklog}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"title":"Updated Title","domain":"operations","urgency":0.9}`
	req := httptest.NewRequest("PATCH", "/api/v1/backlog/"+item.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var got store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&got)
	if got.Title != "Updated Title" {
		t.Errorf("expected 'Updated Title', got '%s'", got.Title)
	}
	if got.Domain != "operations" {
		t.Errorf("expected domain 'operations', got '%s'", got.Domain)
	}
}

func TestDeleteBacklogItem(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Delete Me", ItemType: "task", Status: store.BacklogStatusBacklog}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("DELETE", "/api/v1/backlog/"+item.ID.String(), nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Verify cancelled
	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusCancelled {
		t.Errorf("expected status 'cancelled', got '%s'", updated.Status)
	}
}

// --- Lifecycle Transition Tests ---

func TestBacklogStartTransition(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Start Me", ItemType: "task", Status: store.BacklogStatusReady}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/start", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusInDiscovery {
		t.Errorf("expected status 'in_discovery', got '%s'", updated.Status)
	}
}

func TestBacklogStartRejectsWrongState(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Not Ready", ItemType: "task", Status: store.BacklogStatusBacklog}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/start", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for backlog→start, got %d", w.Code)
	}
}

func TestBacklogDiscoveryComplete(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Discover Me", ItemType: "task", Status: store.BacklogStatusInDiscovery}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"impact":0.9,"urgency":0.8,"estimated_tokens":5000,"labels":["architecture"]}`
	req := httptest.NewRequest("PATCH", "/api/v1/backlog/"+item.ID.String()+"/discovery-complete", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result store.BacklogDiscoveryCompleteResult
	_ = json.NewDecoder(w.Body).Decode(&result)

	if result.Item == nil {
		t.Fatal("expected item in result")
	}
	if result.Item.Status != store.BacklogStatusPlanned {
		t.Errorf("expected status 'planned', got '%s'", result.Item.Status)
	}
}

func TestBacklogDiscoveryCompletePark(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Park Me", ItemType: "task", Status: store.BacklogStatusInDiscovery}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"park":true,"impact":0.3}`
	req := httptest.NewRequest("PATCH", "/api/v1/backlog/"+item.ID.String()+"/discovery-complete", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var result store.BacklogDiscoveryCompleteResult
	_ = json.NewDecoder(w.Body).Decode(&result)
	if result.Item.Status != store.BacklogStatusBacklog {
		t.Errorf("expected status 'backlog' after park, got '%s'", result.Item.Status)
	}
}

func TestBacklogDiscoveryCompleteRejectsWrongState(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Wrong State", ItemType: "task", Status: store.BacklogStatusReady}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"impact":0.5}`
	req := httptest.NewRequest("PATCH", "/api/v1/backlog/"+item.ID.String()+"/discovery-complete", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for ready item, got %d", w.Code)
	}
}

func TestBacklogBeginExecution(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Execute Me", ItemType: "task", Status: store.BacklogStatusPlanned}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/begin-execution", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusInProgress {
		t.Errorf("expected 'in_progress', got '%s'", updated.Status)
	}
}

func TestBacklogBeginExecutionRejectsWrongState(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Not Planned", ItemType: "task", Status: store.BacklogStatusReady}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/begin-execution", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409 for ready→begin-execution, got %d", w.Code)
	}
}

func TestBacklogComplete(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Complete Me", ItemType: "task", Status: store.BacklogStatusInProgress}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/complete", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusDone {
		t.Errorf("expected 'done', got '%s'", updated.Status)
	}
}

func TestBacklogCompleteRejectsWrongState(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Not Started", ItemType: "task", Status: store.BacklogStatusPlanned}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/complete", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestBacklogBlock(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Block Me", ItemType: "task", Status: store.BacklogStatusInProgress}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"reason":"waiting for dependency"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/block", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusBlocked {
		t.Errorf("expected 'blocked', got '%s'", updated.Status)
	}
}

func TestBacklogPark(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	urgency := 0.8
	item := &store.BacklogItem{Title: "Park Me", ItemType: "task", Status: store.BacklogStatusInDiscovery, Urgency: &urgency}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/park", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	updated := ms.backlogItems[item.ID]
	if updated.Status != store.BacklogStatusBacklog {
		t.Errorf("expected 'backlog', got '%s'", updated.Status)
	}
	if updated.PriorityScore == nil {
		t.Error("expected priority_score to be set after park re-scoring")
	}
}

// --- Next endpoint ---

func TestBacklogNext(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	score1 := 0.9
	score2 := 0.5
	_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{Title: "High", ItemType: "task", Status: store.BacklogStatusReady, PriorityScore: &score1})
	_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{Title: "Low", ItemType: "task", Status: store.BacklogStatusReady, PriorityScore: &score2, OneWayDoor: true})
	_ = ms.CreateBacklogItem(context.Background(), &store.BacklogItem{Title: "Not Ready", ItemType: "task", Status: store.BacklogStatusBacklog})

	req := httptest.NewRequest("GET", "/api/v1/backlog/next", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var items []BacklogNextItem
	_ = json.NewDecoder(w.Body).Decode(&items)
	if len(items) != 2 {
		t.Errorf("expected 2 ready items, got %d", len(items))
	}

	// Check model tier information is included
	for _, item := range items {
		if item.ModelTier == "" {
			t.Error("expected model_tier to be set")
		}
		if item.RecommendedModel == "" {
			t.Error("expected recommended_model to be set")
		}
		if item.RoutingMethod == "" {
			t.Error("expected routing_method to be set")
		}
		if item.Runtime == "" {
			t.Error("expected runtime to be set")
		}
		
		// OneWayDoor should result in premium tier
		if item.OneWayDoor && item.ModelTier != "premium" {
			t.Errorf("expected one-way-door item to have premium tier, got %s", item.ModelTier)
		}
		
		// Recommended model should be from the tier
		if item.ModelTier == "premium" && item.RecommendedModel != "anthropic/claude-opus-4-20250514" {
			t.Errorf("expected premium tier to recommend claude-opus, got %s", item.RecommendedModel)
		}
		if item.ModelTier == "standard" && item.RecommendedModel != "anthropic/claude-sonnet-4-20250514" {
			t.Errorf("expected standard tier to recommend claude-sonnet, got %s", item.RecommendedModel)
		}
	}
}

// --- Dependencies Tests ---

func TestCreateDependency(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	blocker := &store.BacklogItem{Title: "Blocker", ItemType: "task", Status: store.BacklogStatusBacklog}
	blocked := &store.BacklogItem{Title: "Blocked", ItemType: "task", Status: store.BacklogStatusBacklog}
	_ = ms.CreateBacklogItem(context.Background(), blocker)
	_ = ms.CreateBacklogItem(context.Background(), blocked)

	body, _ := json.Marshal(map[string]string{
		"blocker_id": blocker.ID.String(),
		"blocked_id": blocked.ID.String(),
	})
	req := httptest.NewRequest("POST", "/api/v1/backlog/dependencies", bytes.NewReader(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var dep store.BacklogDependency
	_ = json.NewDecoder(w.Body).Decode(&dep)
	if dep.BlockerID != blocker.ID {
		t.Errorf("expected blocker_id=%s, got %s", blocker.ID, dep.BlockerID)
	}
}

func TestListDependenciesForItem(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Item", ItemType: "task", Status: store.BacklogStatusBacklog}
	_ = ms.CreateBacklogItem(context.Background(), item)

	dep := &store.BacklogDependency{BlockerID: uuid.New(), BlockedID: item.ID}
	_ = ms.CreateDependency(context.Background(), dep)

	req := httptest.NewRequest("GET", "/api/v1/backlog/"+item.ID.String()+"/dependencies", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	var deps []store.BacklogDependency
	_ = json.NewDecoder(w.Body).Decode(&deps)
	if len(deps) != 1 {
		t.Errorf("expected 1 dependency, got %d", len(deps))
	}
}

// --- Full Backlog Lifecycle Test ---

func TestFullBacklogLifecycle(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	// 1. Create backlog item
	body := `{"title":"E2E Backlog Item","item_type":"story","domain":"product","impact":0.7,"urgency":0.6}`
	req := httptest.NewRequest("POST", "/api/v1/backlog", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created store.BacklogItem
	_ = json.NewDecoder(w.Body).Decode(&created)
	itemID := created.ID.String()

	// 2. Move to ready (via update)
	ms.backlogItems[created.ID].Status = store.BacklogStatusReady

	// 3. Start discovery
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/start", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("start: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ms.backlogItems[created.ID].Status != store.BacklogStatusInDiscovery {
		t.Fatalf("start: expected in_discovery, got %s", ms.backlogItems[created.ID].Status)
	}

	// 4. Discovery complete
	body = `{"impact":0.9,"urgency":0.85,"estimated_tokens":10000}`
	req = httptest.NewRequest("PATCH", "/api/v1/backlog/"+itemID+"/discovery-complete", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("discovery-complete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ms.backlogItems[created.ID].Status != store.BacklogStatusPlanned {
		t.Fatalf("discovery: expected planned, got %s", ms.backlogItems[created.ID].Status)
	}

	// 5. Begin execution
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/begin-execution", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("begin-execution: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ms.backlogItems[created.ID].Status != store.BacklogStatusInProgress {
		t.Fatalf("begin: expected in_progress, got %s", ms.backlogItems[created.ID].Status)
	}

	// 6. Complete
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/complete", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d: %s", w.Code, w.Body.String())
	}
	if ms.backlogItems[created.ID].Status != store.BacklogStatusDone {
		t.Fatalf("complete: expected done, got %s", ms.backlogItems[created.ID].Status)
	}
}

// --- Override Tests ---

func TestCreateOverrideRequiresAdminToken(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	body := `{"override_type":"priority","new_value":"0.9","overridden_by":"mike"}`
	req := httptest.NewRequest("POST", "/api/v1/overrides", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401 without admin token, got %d", w.Code)
	}
}

func TestCreateOverrideWithAdminToken(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	body := `{"override_type":"priority","new_value":"0.9","overridden_by":"mike","reason":"urgent customer request"}`
	req := httptest.NewRequest("POST", "/api/v1/overrides", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}
}

// --- Autonomy Metrics Tests ---

func TestAutonomyMetricsRequiresAdminToken(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/autonomy/metrics", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAutonomyMetricsWithAdminToken(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/autonomy/metrics?days=7", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestBacklogNextWithModelTierRouting(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	// Create items with different characteristics for tier routing
	tests := []struct {
		name     string
		item     *store.BacklogItem
		wantTier string
		wantRuntime string
	}{
		{
			name:     "OneWayDoor item gets premium tier",
			item:     &store.BacklogItem{Title: "Critical Change", ItemType: "task", Status: store.BacklogStatusReady, OneWayDoor: true},
			wantTier: "premium",
			wantRuntime: "openclaw",
		},
		{
			name:     "Regular item gets standard tier",
			item:     &store.BacklogItem{Title: "Regular Task", ItemType: "task", Status: store.BacklogStatusReady},
			wantTier: "standard",
			wantRuntime: "picoclaw", // standard with 0 file patterns gets picoclaw
		},
		{
			name:     "Config-labeled item gets economy tier",
			item:     &store.BacklogItem{Title: "Config Update", ItemType: "task", Status: store.BacklogStatusReady, Labels: []string{"config"}},
			wantTier: "economy", // based on ColdStartRules
			wantRuntime: "picoclaw",
		},
	}

	// Create all test items
	for _, test := range tests {
		_ = ms.CreateBacklogItem(context.Background(), test.item)
	}

	req := httptest.NewRequest("GET", "/api/v1/backlog/next", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var items []BacklogNextItem
	_ = json.NewDecoder(w.Body).Decode(&items)
	if len(items) != len(tests) {
		t.Fatalf("expected %d items, got %d", len(tests), len(items))
	}

	// Build a map by title to check specific items
	itemsByTitle := make(map[string]*BacklogNextItem)
	for i := range items {
		itemsByTitle[items[i].Title] = &items[i]
	}

	for _, test := range tests {
		item, found := itemsByTitle[test.item.Title]
		if !found {
			t.Errorf("item %q not found in response", test.item.Title)
			continue
		}

		if item.ModelTier != test.wantTier {
			t.Errorf("item %q: expected tier %q, got %q", test.item.Title, test.wantTier, item.ModelTier)
		}

		if item.Runtime != test.wantRuntime {
			t.Errorf("item %q: expected runtime %q, got %q", test.item.Title, test.wantRuntime, item.Runtime)
		}

		if item.RecommendedModel == "" {
			t.Errorf("item %q: expected recommended_model to be set", test.item.Title)
		}

		if item.RoutingMethod != "cold_start" {
			t.Errorf("item %q: expected routing_method 'cold_start', got %q", test.item.Title, item.RoutingMethod)
		}

		// Verify the embedded BacklogItem is intact
		if item.BacklogItem.ID != test.item.ID {
			t.Errorf("item %q: BacklogItem ID mismatch", test.item.Title)
		}
		if item.BacklogItem.Title != test.item.Title {
			t.Errorf("item %q: BacklogItem Title mismatch", test.item.Title)
		}
	}
}
