//go:build integration

package store

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
)

func setupTestDB(t *testing.T) *PostgresStore {
	t.Helper()
	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		t.Skip("DATABASE_URL not set, skipping integration test")
	}

	ctx := context.Background()
	s, err := NewPostgresStore(ctx, dbURL)
	if err != nil {
		t.Fatalf("failed to connect: %v", err)
	}

	t.Cleanup(func() {
		// Truncate in dependency order
		_, _ = s.pool.Exec(ctx, "TRUNCATE swarm_task_events CASCADE")
		_, _ = s.pool.Exec(ctx, "TRUNCATE swarm_tasks CASCADE")
		s.Close()
	})

	return s
}

func TestCreateAndGetTask(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	task := &Task{
		Title:                "Integration Test Task",
		Description:          "Verify create and get round-trip",
		Owner:                "test-owner",
		RequiredCapabilities: []string{"code", "research"},
		Status:               StatusPending,
		TimeoutSeconds:       120,
		MaxRetries:           2,
		RetryEligible:        true,
		Priority:             5,
		Source:                "integration-test",
		Metadata:             map[string]interface{}{"key": "value"},
	}

	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}
	if task.ID == uuid.Nil {
		t.Fatal("expected non-nil task ID after create")
	}
	if task.CreatedAt.IsZero() {
		t.Fatal("expected CreatedAt to be set")
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if got == nil {
		t.Fatal("expected task, got nil")
	}
	if got.Title != "Integration Test Task" {
		t.Errorf("expected title 'Integration Test Task', got '%s'", got.Title)
	}
	if got.Owner != "test-owner" {
		t.Errorf("expected owner 'test-owner', got '%s'", got.Owner)
	}
	if len(got.RequiredCapabilities) != 2 {
		t.Errorf("expected 2 capabilities, got %d", len(got.RequiredCapabilities))
	}
	if got.Status != StatusPending {
		t.Errorf("expected status pending, got %s", got.Status)
	}
	if got.Priority != 5 {
		t.Errorf("expected priority 5, got %d", got.Priority)
	}
	if got.Metadata["key"] != "value" {
		t.Errorf("expected metadata key=value, got %v", got.Metadata)
	}
}

func TestListTasksWithFilters(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	tasks := []*Task{
		{Title: "Task A", Owner: "alice", Source: "manual", Status: StatusPending, Priority: 3},
		{Title: "Task B", Owner: "bob", Source: "agent", Status: StatusPending, Priority: 5},
		{Title: "Task C", Owner: "alice", Source: "manual", Status: StatusCompleted, Priority: 1},
	}
	for _, task := range tasks {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	// Filter by owner
	result, err := s.ListTasks(ctx, TaskFilter{Owner: "alice"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 tasks for alice, got %d", len(result))
	}

	// Filter by source
	result, err = s.ListTasks(ctx, TaskFilter{Source: "agent"})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 task from agent, got %d", len(result))
	}

	// Filter by status
	pending := StatusPending
	result, err = s.ListTasks(ctx, TaskFilter{Status: &pending})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(result) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(result))
	}

	// Combined filter: owner + status
	result, err = s.ListTasks(ctx, TaskFilter{Owner: "alice", Status: &pending})
	if err != nil {
		t.Fatalf("ListTasks failed: %v", err)
	}
	if len(result) != 1 {
		t.Errorf("expected 1 pending alice task, got %d", len(result))
	}
}

func TestUpdateTask(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	task := &Task{
		Title:    "Update Me",
		Owner:    "system",
		Status:   StatusPending,
		Priority: 1,
		Source:   "manual",
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	now := time.Now()
	task.Status = StatusAssigned
	task.AssignedAgent = "worker-1"
	task.AssignedAt = &now
	task.Priority = 8

	if err := s.UpdateTask(ctx, task); err != nil {
		t.Fatalf("UpdateTask failed: %v", err)
	}

	got, err := s.GetTask(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTask failed: %v", err)
	}
	if got.Status != StatusAssigned {
		t.Errorf("expected status assigned, got %s", got.Status)
	}
	if got.AssignedAgent != "worker-1" {
		t.Errorf("expected agent 'worker-1', got '%s'", got.AssignedAgent)
	}
	if got.Priority != 8 {
		t.Errorf("expected priority 8, got %d", got.Priority)
	}
}

func TestGetPendingTasks(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	tasks := []*Task{
		{Title: "Pending 1", Owner: "sys", Status: StatusPending, Source: "manual"},
		{Title: "Pending 2", Owner: "sys", Status: StatusPending, Source: "manual"},
		{Title: "Completed", Owner: "sys", Status: StatusCompleted, Source: "manual"},
		{Title: "In Progress", Owner: "sys", Status: StatusInProgress, Source: "manual"},
	}
	for _, task := range tasks {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	pending, err := s.GetPendingTasks(ctx)
	if err != nil {
		t.Fatalf("GetPendingTasks failed: %v", err)
	}
	if len(pending) != 2 {
		t.Errorf("expected 2 pending tasks, got %d", len(pending))
	}
	for _, p := range pending {
		if p.Status != StatusPending {
			t.Errorf("expected pending status, got %s", p.Status)
		}
	}
}

func TestGetActiveTasksForAgent(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	tasks := []*Task{
		{Title: "Assigned to A", Owner: "sys", Status: StatusAssigned, AssignedAgent: "agent-a", Source: "manual"},
		{Title: "In progress A", Owner: "sys", Status: StatusInProgress, AssignedAgent: "agent-a", Source: "manual"},
		{Title: "Completed A", Owner: "sys", Status: StatusCompleted, AssignedAgent: "agent-a", Source: "manual"},
		{Title: "Assigned to B", Owner: "sys", Status: StatusAssigned, AssignedAgent: "agent-b", Source: "manual"},
	}
	for _, task := range tasks {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
	}

	active, err := s.GetActiveTasksForAgent(ctx, "agent-a")
	if err != nil {
		t.Fatalf("GetActiveTasksForAgent failed: %v", err)
	}
	if len(active) != 2 {
		t.Errorf("expected 2 active tasks for agent-a, got %d", len(active))
	}

	active, err = s.GetActiveTasksForAgent(ctx, "agent-b")
	if err != nil {
		t.Fatalf("GetActiveTasksForAgent failed: %v", err)
	}
	if len(active) != 1 {
		t.Errorf("expected 1 active task for agent-b, got %d", len(active))
	}
}

func TestCreateAndGetTaskEvents(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	task := &Task{
		Title:  "Event Test",
		Owner:  "sys",
		Status: StatusPending,
		Source: "manual",
	}
	if err := s.CreateTask(ctx, task); err != nil {
		t.Fatalf("CreateTask failed: %v", err)
	}

	events := []*TaskEvent{
		{TaskID: task.ID, Event: "created", AgentID: "system"},
		{TaskID: task.ID, Event: "assigned", AgentID: "worker-1", Payload: map[string]interface{}{"reason": "capability match"}},
		{TaskID: task.ID, Event: "completed", AgentID: "worker-1"},
	}
	for _, e := range events {
		if err := s.CreateTaskEvent(ctx, e); err != nil {
			t.Fatalf("CreateTaskEvent failed: %v", err)
		}
		if e.ID == uuid.Nil {
			t.Error("expected event ID to be set")
		}
	}

	got, err := s.GetTaskEvents(ctx, task.ID)
	if err != nil {
		t.Fatalf("GetTaskEvents failed: %v", err)
	}
	if len(got) != 3 {
		t.Fatalf("expected 3 events, got %d", len(got))
	}
	if got[0].Event != "created" {
		t.Errorf("expected first event 'created', got '%s'", got[0].Event)
	}
	if got[1].Payload["reason"] != "capability match" {
		t.Errorf("expected payload reason, got %v", got[1].Payload)
	}
}

func TestGetStats(t *testing.T) {
	s := setupTestDB(t)
	ctx := context.Background()

	now := time.Now()
	assignedAt := now.Add(-10 * time.Second)
	tasks := []*Task{
		{Title: "Pending", Owner: "sys", Status: StatusPending, Source: "manual"},
		{Title: "In Progress", Owner: "sys", Status: StatusInProgress, Source: "manual"},
		{Title: "Completed 1", Owner: "sys", Status: StatusCompleted, Source: "manual", AssignedAt: &assignedAt, CompletedAt: &now},
		{Title: "Completed 2", Owner: "sys", Status: StatusCompleted, Source: "manual", AssignedAt: &assignedAt, CompletedAt: &now},
		{Title: "Failed", Owner: "sys", Status: StatusFailed, Source: "manual"},
	}
	for _, task := range tasks {
		if err := s.CreateTask(ctx, task); err != nil {
			t.Fatalf("CreateTask failed: %v", err)
		}
		// For completed tasks, we need to update to set assigned_at/completed_at
		if task.AssignedAt != nil {
			if err := s.UpdateTask(ctx, task); err != nil {
				t.Fatalf("UpdateTask failed: %v", err)
			}
		}
	}

	stats, err := s.GetStats(ctx)
	if err != nil {
		t.Fatalf("GetStats failed: %v", err)
	}
	if stats.TotalPending != 1 {
		t.Errorf("expected 1 pending, got %d", stats.TotalPending)
	}
	if stats.TotalInProgress != 1 {
		t.Errorf("expected 1 in_progress, got %d", stats.TotalInProgress)
	}
	if stats.TotalCompleted != 2 {
		t.Errorf("expected 2 completed, got %d", stats.TotalCompleted)
	}
	if stats.TotalFailed != 1 {
		t.Errorf("expected 1 failed, got %d", stats.TotalFailed)
	}
	if stats.AvgCompletionMs <= 0 {
		t.Errorf("expected positive avg completion time, got %f", stats.AvgCompletionMs)
	}
}
