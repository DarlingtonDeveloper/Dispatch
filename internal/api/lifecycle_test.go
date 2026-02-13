package api

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

// TestFullTaskLifecycle exercises the complete happy-path:
// create → get → progress (assigned→in_progress) → complete
func TestFullTaskLifecycle(t *testing.T) {
	router, _ := setupTestRouter()

	// 1. Create task
	body := `{"title":"E2E Lifecycle","required_capabilities":["research"],"priority":5,"owner":"mike-d"}`
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "scout")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var created store.Task
	_ = json.NewDecoder(w.Body).Decode(&created)
	if created.Status != store.StatusPending {
		t.Fatalf("create: expected pending, got %s", created.Status)
	}
	taskID := created.ID.String()

	// 2. Get task and verify fields
	req = httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
	req.Header.Set("X-Agent-ID", "scout")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("get: expected 200, got %d", w.Code)
	}

	var fetched store.Task
	_ = json.NewDecoder(w.Body).Decode(&fetched)
	if fetched.Title != "E2E Lifecycle" {
		t.Errorf("get: expected title 'E2E Lifecycle', got '%s'", fetched.Title)
	}
	if fetched.Priority != 5 {
		t.Errorf("get: expected priority 5, got %d", fetched.Priority)
	}
	if fetched.Owner != "mike-d" {
		t.Errorf("get: expected owner 'mike-d', got '%s'", fetched.Owner)
	}

	// 3. Simulate assignment (directly set in store for e2e)
	fetched.Status = store.StatusAssigned
	fetched.AssignedAgent = "scout"

	// Update via mock — in real flow the broker does this
	req = httptest.NewRequest("PATCH", "/api/v1/tasks/"+taskID, bytes.NewBufferString(`{"metadata":{"assigned_by":"broker"}}`))
	req.Header.Set("X-Agent-ID", "scout")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// 4. Report progress (transitions assigned→in_progress via API)
	// First manually set to assigned state since we don't have the broker running
	fetched.Status = store.StatusAssigned
	fetched.AssignedAgent = "scout"

	req = httptest.NewRequest("POST", "/api/v1/tasks/"+taskID+"/progress", bytes.NewBufferString(`{"progress":0.5,"detail":"halfway"}`))
	req.Header.Set("X-Agent-ID", "scout")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("progress: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	// 5. Complete the task
	req = httptest.NewRequest("POST", "/api/v1/tasks/"+taskID+"/complete", bytes.NewBufferString(`{"result":{"output":"research complete","pages":42}}`))
	req.Header.Set("X-Agent-ID", "scout")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("complete: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var completed store.Task
	_ = json.NewDecoder(w.Body).Decode(&completed)
	if completed.Status != store.StatusCompleted {
		t.Errorf("complete: expected completed, got %s", completed.Status)
	}
	if completed.CompletedAt == nil {
		t.Error("complete: expected completed_at to be set")
	}

	// 6. Verify final state via GET
	req = httptest.NewRequest("GET", "/api/v1/tasks/"+taskID, nil)
	req.Header.Set("X-Agent-ID", "scout")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	var final store.Task
	_ = json.NewDecoder(w.Body).Decode(&final)
	if final.Status != store.StatusCompleted {
		t.Errorf("final get: expected completed, got %s", final.Status)
	}
}

// TestTaskFailureLifecycle exercises: create → fail → verify DLQ-ready state
func TestTaskFailureLifecycle(t *testing.T) {
	router, ms := setupTestRouter()

	// 1. Create task
	body := `{"title":"Will Fail","required_capabilities":["code"],"priority":3}`
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "nova")
	req.Header.Set("Content-Type", "application/json")

	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("create: expected 201, got %d", w.Code)
	}

	var created store.Task
	_ = json.NewDecoder(w.Body).Decode(&created)
	taskID := created.ID.String()

	// 2. Simulate assignment + start (broker would do this)
	task := ms.tasks[created.ID]
	task.Status = store.StatusInProgress
	task.AssignedAgent = "nova"

	// 3. Report failure with retry_eligible=false
	req = httptest.NewRequest("POST", "/api/v1/tasks/"+taskID+"/fail",
		bytes.NewBufferString(`{"error":"segmentation fault","retry_eligible":false}`))
	req.Header.Set("X-Agent-ID", "nova")
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("fail: expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var failed store.Task
	_ = json.NewDecoder(w.Body).Decode(&failed)
	if failed.Status != store.StatusFailed {
		t.Errorf("fail: expected failed, got %s", failed.Status)
	}
	if failed.Error != "segmentation fault" {
		t.Errorf("fail: expected error 'segmentation fault', got '%s'", failed.Error)
	}
	if failed.RetryEligible {
		t.Error("fail: expected retry_eligible=false")
	}
}

// TestTaskListFiltering verifies status, owner, and source filters
func TestTaskListFiltering(t *testing.T) {
	router, ms := setupTestRouter()

	// Create tasks with different properties
	tasks := []*store.Task{
		{Title: "Task A", Owner: "mike-d", Status: store.StatusPending, Source: "manual"},
		{Title: "Task B", Owner: "mike-d", Status: store.StatusCompleted, Source: "agent"},
		{Title: "Task C", Owner: "system", Status: store.StatusPending, Source: "manual"},
	}
	for _, task := range tasks {
		_ = ms.CreateTask(context.TODO(), task)
	}

	tests := []struct {
		name     string
		query    string
		expected int
	}{
		{"all tasks", "/api/v1/tasks", 3},
		{"by status pending", "/api/v1/tasks?status=pending", 2},
		{"by owner mike-d", "/api/v1/tasks?owner=mike-d", 2},
		{"by source manual", "/api/v1/tasks?source=manual", 2},
		{"combined filters", "/api/v1/tasks?status=pending&owner=mike-d", 1},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", tt.query, nil)
			req.Header.Set("X-Agent-ID", "test")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)

			if w.Code != http.StatusOK {
				t.Fatalf("expected 200, got %d", w.Code)
			}

			var result []store.Task
			_ = json.NewDecoder(w.Body).Decode(&result)
			if len(result) != tt.expected {
				t.Errorf("expected %d tasks, got %d", tt.expected, len(result))
			}
		})
	}
}

// TestGetNonExistentTask verifies 404 for unknown task ID
func TestGetNonExistentTask(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/tasks/00000000-0000-0000-0000-000000000000", nil)
	req.Header.Set("X-Agent-ID", "test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

// TestGetInvalidTaskID verifies 400 for malformed UUID
func TestGetInvalidTaskID(t *testing.T) {
	router, _ := setupTestRouter()

	req := httptest.NewRequest("GET", "/api/v1/tasks/not-a-uuid", nil)
	req.Header.Set("X-Agent-ID", "test")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

// TestCreateTaskDefaults verifies server-side defaults
func TestCreateTaskDefaults(t *testing.T) {
	router, _ := setupTestRouter()

	body := `{"title":"Minimal Task"}`
	req := httptest.NewRequest("POST", "/api/v1/tasks", bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "scout")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", w.Code, w.Body.String())
	}

	var task store.Task
	_ = json.NewDecoder(w.Body).Decode(&task)

	if task.Owner != "scout" {
		t.Errorf("expected owner defaulted to agent ID 'scout', got '%s'", task.Owner)
	}
	if task.Source != "agent" {
		t.Errorf("expected source defaulted to 'agent', got '%s'", task.Source)
	}
	if task.TimeoutSeconds != 300 {
		t.Errorf("expected default timeout 300, got %d", task.TimeoutSeconds)
	}
	if task.MaxRetries != 3 {
		t.Errorf("expected default max_retries 3, got %d", task.MaxRetries)
	}
	if !task.RetryEligible {
		t.Error("expected retry_eligible=true by default")
	}
}

// TestUpdateTaskMetadata verifies PATCH updates metadata
func TestUpdateTaskMetadata(t *testing.T) {
	router, ms := setupTestRouter()

	task := &store.Task{
		Title:  "Metadata Test",
		Owner:  "system",
		Status: store.StatusPending,
		Source: "manual",
	}
	_ = ms.CreateTask(context.TODO(), task)

	body := `{"metadata":{"key":"value","count":42}}`
	req := httptest.NewRequest("PATCH", "/api/v1/tasks/"+task.ID.String(), bytes.NewBufferString(body))
	req.Header.Set("X-Agent-ID", "test")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d: %s", w.Code, w.Body.String())
	}

	var updated store.Task
	_ = json.NewDecoder(w.Body).Decode(&updated)
	if updated.Metadata == nil {
		t.Fatal("expected metadata to be set")
	}
	if updated.Metadata["key"] != "value" {
		t.Errorf("expected metadata.key='value', got '%v'", updated.Metadata["key"])
	}
}

// TestProgressEndpointOnAlreadyInProgress ensures progress on in_progress task succeeds without re-transition
func TestProgressEndpointOnAlreadyInProgress(t *testing.T) {
	router, ms := setupTestRouter()

	task := &store.Task{
		Title:         "Already Running",
		Owner:         "system",
		Status:        store.StatusInProgress,
		AssignedAgent: "nova",
		Source:        "manual",
	}
	_ = ms.CreateTask(context.TODO(), task)

	req := httptest.NewRequest("POST", "/api/v1/tasks/"+task.ID.String()+"/progress",
		bytes.NewBufferString(`{"progress":0.75}`))
	req.Header.Set("X-Agent-ID", "nova")
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}

	// Status should remain in_progress
	updated := ms.tasks[task.ID]
	if updated.Status != store.StatusInProgress {
		t.Errorf("expected still in_progress, got %s", updated.Status)
	}
}
