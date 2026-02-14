package store

import (
	"context"
	"time"

	"github.com/google/uuid"
)

type TaskStatus string

const (
	StatusPending    TaskStatus = "pending"
	StatusAssigned   TaskStatus = "assigned"
	StatusInProgress TaskStatus = "in_progress"
	StatusCompleted  TaskStatus = "completed"
	StatusFailed     TaskStatus = "failed"
	StatusTimedOut   TaskStatus = "timed_out"
)

type Task struct {
	ID                   uuid.UUID              `json:"task_id"`
	Title                string                 `json:"title"`
	Description          string                 `json:"description,omitempty"`
	Owner                string                 `json:"owner"`
	RequiredCapabilities []string               `json:"required_capabilities"`

	// State
	Status        TaskStatus `json:"status"`
	AssignedAgent string     `json:"assigned_agent,omitempty"`

	// Timestamps
	CreatedAt   time.Time  `json:"created_at"`
	AssignedAt  *time.Time `json:"assigned_at,omitempty"`
	StartedAt   *time.Time `json:"started_at,omitempty"`
	CompletedAt *time.Time `json:"completed_at,omitempty"`
	UpdatedAt   time.Time  `json:"updated_at"`

	// Result
	Result map[string]interface{} `json:"result,omitempty"`
	Error  string                 `json:"error,omitempty"`

	// Retry
	RetryCount    int  `json:"retry_count"`
	MaxRetries    int  `json:"max_retries"`
	RetryEligible bool `json:"retry_eligible"`

	// Timeout
	TimeoutSeconds int `json:"timeout_seconds"`

	// Metadata
	Priority     int                    `json:"priority"`
	Source       string                 `json:"source"`
	ParentTaskID *uuid.UUID             `json:"parent_task_id,omitempty"`
	Metadata     map[string]interface{} `json:"metadata,omitempty"`

	// Scoring v2
	RiskScore               *float64               `json:"risk_score,omitempty"`
	CostEstimateTokens      *int64                 `json:"cost_estimate_tokens,omitempty"`
	CostEstimateUSD         *float64               `json:"cost_estimate_usd,omitempty"`
	VerifiabilityScore      *float64               `json:"verifiability_score,omitempty"`
	ReversibilityScore      *float64               `json:"reversibility_score,omitempty"`
	OversightLevel          string                 `json:"oversight_level,omitempty"`
	ScoringFactors          map[string]interface{} `json:"scoring_factors,omitempty"`
	ScoringVersion          int                    `json:"scoring_version"`
	ComplexityScore         *float64               `json:"complexity_score,omitempty"`
	UncertaintyScore        *float64               `json:"uncertainty_score,omitempty"`
	DurationClass           string                 `json:"duration_class,omitempty"`
	ContextualityScore      *float64               `json:"contextuality_score,omitempty"`
	SubjectivityScore       *float64               `json:"subjectivity_score,omitempty"`
	FastPath                bool                   `json:"fast_path"`
	ParetoFrontier          map[string]interface{} `json:"pareto_frontier,omitempty"`
	AlternativeDecompositions map[string]interface{} `json:"alternative_decompositions,omitempty"`
}

type TaskFilter struct {
	Status *TaskStatus
	Owner  string
	Agent  string
	Source string
	Limit  int
	Offset int
}

type TaskEvent struct {
	ID        uuid.UUID              `json:"id"`
	TaskID    uuid.UUID              `json:"task_id"`
	Event     string                 `json:"event"`
	AgentID   string                 `json:"agent_id,omitempty"`
	Payload   map[string]interface{} `json:"payload,omitempty"`
	CreatedAt time.Time              `json:"created_at"`
}

type TaskStats struct {
	TotalPending    int     `json:"total_pending"`
	TotalInProgress int     `json:"total_in_progress"`
	TotalCompleted  int     `json:"total_completed"`
	TotalFailed     int     `json:"total_failed"`
	AvgCompletionMs float64 `json:"avg_completion_ms"`
}

type AgentTaskHistory struct {
	ID              uuid.UUID  `json:"id"`
	AgentSlug       string     `json:"agent_slug"`
	TaskID          uuid.UUID  `json:"task_id"`
	StartedAt       *time.Time `json:"started_at,omitempty"`
	CompletedAt     *time.Time `json:"completed_at,omitempty"`
	DurationSeconds *float64   `json:"duration_seconds,omitempty"`
	TokensUsed      *int64     `json:"tokens_used,omitempty"`
	CostUSD         *float64   `json:"cost_usd,omitempty"`
	Success         *bool      `json:"success,omitempty"`
	CreatedAt       time.Time  `json:"created_at"`
}

type Store interface {
	CreateTask(ctx context.Context, task *Task) error
	GetTask(ctx context.Context, id uuid.UUID) (*Task, error)
	ListTasks(ctx context.Context, filter TaskFilter) ([]*Task, error)
	UpdateTask(ctx context.Context, task *Task) error

	GetPendingTasks(ctx context.Context) ([]*Task, error)
	GetActiveTasksForAgent(ctx context.Context, agentID string) ([]*Task, error)
	GetActiveTasks(ctx context.Context) ([]*Task, error)

	CreateTaskEvent(ctx context.Context, event *TaskEvent) error
	GetTaskEvents(ctx context.Context, taskID uuid.UUID) ([]*TaskEvent, error)

	GetStats(ctx context.Context) (*TaskStats, error)

	CreateAgentTaskHistory(ctx context.Context, h *AgentTaskHistory) error
	GetAgentTaskHistory(ctx context.Context, agentSlug string, limit int) ([]*AgentTaskHistory, error)
	GetAgentAvgDuration(ctx context.Context, agentSlug string) (*float64, error)
	GetAgentAvgCost(ctx context.Context, agentSlug string) (*float64, error)

	GetTrustScore(ctx context.Context, agentSlug, category, severity string) (float64, error)

	Close() error
}
