package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

func TestInitStages(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Stage Test", ItemType: "task", Status: store.BacklogStatusPlanned, ModelTier: "standard"}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/init-stages", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]json.RawMessage
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Verify item has stages initialized
	updated := ms.backlogItems[item.ID]
	if len(updated.StageTemplate) == 0 {
		t.Fatal("expected stage_template to be set")
	}
	if updated.CurrentStage != updated.StageTemplate[0] {
		t.Errorf("expected current_stage=%q, got %q", updated.StageTemplate[0], updated.CurrentStage)
	}
	if updated.StageIndex != 0 {
		t.Errorf("expected stage_index=0, got %d", updated.StageIndex)
	}

	// Verify gates were created
	if ms.stageGates[item.ID] == nil {
		t.Fatal("expected gates to be created")
	}
}

func TestInitStagesCustomTemplate(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{Title: "Custom Stages", ItemType: "task", Status: store.BacklogStatusPlanned}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"template":["build","test","deploy"]}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/init-stages", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if len(updated.StageTemplate) != 3 {
		t.Fatalf("expected 3 stages, got %d", len(updated.StageTemplate))
	}
	if updated.StageTemplate[0] != "build" || updated.StageTemplate[1] != "test" || updated.StageTemplate[2] != "deploy" {
		t.Errorf("unexpected template: %v", updated.StageTemplate)
	}
}

func TestAdvanceStage(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Advance Test",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
		UpdatedAt:     time.Now().Add(-1 * time.Minute), // not too recent
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	// Create and satisfy all criteria
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete", "self-review passed"})
	_ = ms.SatisfyAllCriteria(context.Background(), item.ID, "implement", "test-agent")

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if updated.CurrentStage != "verify" {
		t.Errorf("expected current_stage='verify', got %q", updated.CurrentStage)
	}
	if updated.StageIndex != 1 {
		t.Errorf("expected stage_index=1, got %d", updated.StageIndex)
	}
}

func TestAdvanceStageBlocked(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Blocked Advance",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	// Create criteria but don't satisfy them
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete"})

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdvanceStageForce(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Force Advance",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	// Create criteria but don't satisfy them
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete"})

	body := `{"force":true,"reason":"emergency hotfix"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if updated.CurrentStage != "verify" {
		t.Errorf("expected current_stage='verify', got %q", updated.CurrentStage)
	}
}

func TestAdvanceStageForceNoReason(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Force No Reason",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	body := `{"force":true}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdvanceStageVelocityCheck(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Velocity Check",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
		UpdatedAt:     time.Now(), // very recent — triggers velocity check
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	// Create criteria and satisfy them programmatically (no satisfied_by)
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete"})
	// Satisfy without a satisfied_by to simulate programmatic satisfaction
	now := time.Now()
	ms.stageGates[item.ID]["implement"] = []store.GateCriterion{
		{Criterion: "code complete", Satisfied: true, SatisfiedAt: &now, SatisfiedBy: ""},
	}

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 for velocity check, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSatisfyGateSingle(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Satisfy Single",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete", "self-review passed"})

	body := `{"criterion":"code complete","satisfied_by":"test-agent"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["all_met"] == true {
		t.Error("expected all_met=false since only one criterion satisfied")
	}
}

func TestSatisfyGateAll(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Satisfy All",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete", "self-review passed"})

	body := `{"all":true,"satisfied_by":"test-agent"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["all_met"] != true {
		t.Errorf("expected all_met=true, got %v", resp["all_met"])
	}
}

func TestGateStatus(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Gate Status",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete"})
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "verify", []string{"tests passing"})

	// Check current stage status
	req := httptest.NewRequest("GET", "/api/v1/backlog/"+item.ID.String()+"/gate/status", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	if resp["stage"] != "implement" {
		t.Errorf("expected stage='implement', got %v", resp["stage"])
	}

	// Check specific stage status
	req = httptest.NewRequest("GET", "/api/v1/backlog/"+item.ID.String()+"/gate/status?stage=verify", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	_ = json.NewDecoder(w.Body).Decode(&resp)
	if resp["stage"] != "verify" {
		t.Errorf("expected stage='verify', got %v", resp["stage"])
	}
}

func TestStageLifecycle(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	// 1. Create item
	item := &store.BacklogItem{
		Title:    "E2E Stage Lifecycle",
		ItemType: "task",
		Status:   store.BacklogStatusPlanned,
		ModelTier: "economy",
	}
	_ = ms.CreateBacklogItem(context.Background(), item)
	itemID := item.ID.String()

	// 2. Init stages (economy tier: implement, verify)
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/init-stages", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("init-stages: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated := ms.backlogItems[item.ID]
	if len(updated.StageTemplate) != 2 {
		t.Fatalf("expected economy template with 2 stages, got %d: %v", len(updated.StageTemplate), updated.StageTemplate)
	}
	if updated.CurrentStage != "implement" {
		t.Fatalf("expected current_stage='implement', got %q", updated.CurrentStage)
	}

	// 3. Satisfy implement gates
	body := `{"all":true,"satisfied_by":"test-agent"}`
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("satisfy implement: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 4. Advance to verify — need to set UpdatedAt in the past to pass velocity check
	ms.backlogItems[item.ID].UpdatedAt = time.Now().Add(-1 * time.Minute)

	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("advance to verify: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	updated = ms.backlogItems[item.ID]
	if updated.CurrentStage != "verify" {
		t.Fatalf("expected current_stage='verify', got %q", updated.CurrentStage)
	}

	// 5. Satisfy verify gates
	body = `{"all":true,"satisfied_by":"test-agent"}`
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("satisfy verify: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 6. Try to advance past final stage — should get 409
	ms.backlogItems[item.ID].UpdatedAt = time.Now().Add(-1 * time.Minute)
	req = httptest.NewRequest("POST", "/api/v1/backlog/"+itemID+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("advance past final: expected 409, got %d: %s", w.Code, w.Body.String())
	}

	// 7. Verify gate status for verify stage shows all met
	req = httptest.NewRequest("GET", "/api/v1/backlog/"+itemID+"/gate/status", nil)
	req.Header.Set("X-Agent-ID", "test-agent")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("gate status: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var statusResp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&statusResp)
	if statusResp["all_met"] != true {
		t.Errorf("expected all_met=true for final stage, got %v", statusResp["all_met"])
	}
}

// Edge Case Tests

func TestInitStagesNonExistentItem(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	// Use a random UUID that doesn't exist
	nonExistentID := "550e8400-e29b-41d4-a716-446655440000"

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+nonExistentID+"/init-stages", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent item, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAdvanceStageAtFinalStage(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	// Create an item already at the final stage
	item := &store.BacklogItem{
		Title:         "Final Stage Test",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement", "verify"},
		CurrentStage:  "verify", // Already at final stage
		StageIndex:    1,        // Index of final stage
		UpdatedAt:     time.Now().Add(-1 * time.Minute),
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	// Create and satisfy all criteria for the final stage
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "verify", []string{"tests passing"})
	_ = ms.SatisfyAllCriteria(context.Background(), item.ID, "verify", "test-agent")

	// Try to advance past the final stage
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusConflict {
		t.Fatalf("expected 409 when advancing past final stage, got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Check error message mentions final stage
	if errorMsg, ok := resp["error"].(string); ok {
		if !contains(errorMsg, "final stage") && !contains(errorMsg, "last stage") {
			t.Errorf("expected error message to mention final/last stage, got: %s", errorMsg)
		}
	}
}

func TestSatisfyNonExistentCriterion(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:         "Non-Existent Criterion Test",
		ItemType:      "task",
		Status:        store.BacklogStatusPlanned,
		StageTemplate: []string{"implement"},
		CurrentStage:  "implement",
		StageIndex:    0,
	}
	_ = ms.CreateBacklogItem(context.Background(), item)
	_ = ms.CreateGateCriteria(context.Background(), item.ID, "implement", []string{"code complete"})

	// Try to satisfy a criterion that doesn't exist
	body := `{"criterion":"non-existent criterion","satisfied_by":"test-agent"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+item.ID.String()+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful handling), got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Should still report all_met=false since the actual criterion wasn't satisfied
	if resp["all_met"] == true {
		t.Error("expected all_met=false when satisfying non-existent criterion")
	}

	// Verify the actual criterion is still unsatisfied
	criteria, _ := ms.GetGateStatus(context.Background(), item.ID, "implement")
	if len(criteria) != 1 || criteria[0].Satisfied {
		t.Error("expected actual criterion to remain unsatisfied")
	}
}

func TestGateStatusNoStagesInitialized(t *testing.T) {
	router, ms := setupBacklogTestRouter()

	item := &store.BacklogItem{
		Title:    "No Stages Test",
		ItemType: "task",
		Status:   store.BacklogStatusPlanned,
		// No StageTemplate set - stages not initialized
	}
	_ = ms.CreateBacklogItem(context.Background(), item)

	req := httptest.NewRequest("GET", "/api/v1/backlog/"+item.ID.String()+"/gate/status", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 (graceful handling), got %d: %s", w.Code, w.Body.String())
	}

	var resp map[string]interface{}
	_ = json.NewDecoder(w.Body).Decode(&resp)

	// Should handle gracefully - likely return empty stage or indicate no stages
	if stage, exists := resp["stage"]; exists && stage != "" && stage != nil {
		t.Logf("got stage=%v for item with no stages initialized - this is OK if expected", stage)
	}

	// Should have empty or nil criteria
	if criteria, exists := resp["criteria"]; exists {
		if criteriaSlice, ok := criteria.([]interface{}); ok && len(criteriaSlice) > 0 {
			t.Error("expected empty criteria for item with no stages initialized")
		}
	}
}

func TestAdvanceStageNonExistentItem(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	nonExistentID := "550e8400-e29b-41d4-a716-446655440001"

	req := httptest.NewRequest("POST", "/api/v1/backlog/"+nonExistentID+"/advance-stage", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent item, got %d: %s", w.Code, w.Body.String())
	}
}

func TestSatisfyGateNonExistentItem(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	nonExistentID := "550e8400-e29b-41d4-a716-446655440002"

	body := `{"criterion":"some criterion","satisfied_by":"test-agent"}`
	req := httptest.NewRequest("POST", "/api/v1/backlog/"+nonExistentID+"/gate/satisfy", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test-agent")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent item, got %d: %s", w.Code, w.Body.String())
	}
}

func TestGateStatusNonExistentItem(t *testing.T) {
	router, _ := setupBacklogTestRouter()

	nonExistentID := "550e8400-e29b-41d4-a716-446655440003"

	req := httptest.NewRequest("GET", "/api/v1/backlog/"+nonExistentID+"/gate/status", nil)
	req.Header.Set("X-Agent-ID", "test-agent")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for non-existent item, got %d: %s", w.Code, w.Body.String())
	}
}
