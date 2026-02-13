package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

func TestStatsEndpoint_ReturnsStats(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/stats", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var stats store.TaskStats
	if err := json.NewDecoder(w.Body).Decode(&stats); err != nil {
		t.Fatalf("failed to decode stats: %v", err)
	}
	// mockStore.GetStats returns TotalPending: 1
	if stats.TotalPending != 1 {
		t.Errorf("expected TotalPending=1, got %d", stats.TotalPending)
	}
}

func TestAgentsEndpoint_AggregatesInfo(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/agents", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var agents []AgentInfo
	if err := json.NewDecoder(w.Body).Decode(&agents); err != nil {
		t.Fatalf("failed to decode agents: %v", err)
	}
	if len(agents) != 1 {
		t.Fatalf("expected 1 agent, got %d", len(agents))
	}
	if agents[0].Name != "test" {
		t.Errorf("expected agent name 'test', got '%s'", agents[0].Name)
	}
	if agents[0].Status != "ready" {
		t.Errorf("expected status 'ready', got '%s'", agents[0].Status)
	}
	if agents[0].ActiveTasks != 0 {
		t.Errorf("expected 0 active tasks, got %d", agents[0].ActiveTasks)
	}
	if agents[0].Drained {
		t.Error("expected agent not drained")
	}
}

func TestDrainEndpoint(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("POST", "/api/v1/agents/test-agent/drain", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Authorization", "Bearer test-token")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]string
	if err := json.NewDecoder(w.Body).Decode(&resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}
	if resp["status"] != "drained" {
		t.Errorf("expected status 'drained', got '%s'", resp["status"])
	}
	if resp["agent"] != "test-agent" {
		t.Errorf("expected agent 'test-agent', got '%s'", resp["agent"])
	}
}
