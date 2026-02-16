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

	// Model routing
	Labels           []string `json:"labels,omitempty"`
	FilePatterns     []string `json:"file_patterns,omitempty"`
	OneWayDoor       bool     `json:"one_way_door"`
	RecommendedModel string   `json:"recommended_model,omitempty"`
	ModelTier        string   `json:"model_tier,omitempty"`
	RoutingMethod    string   `json:"routing_method,omitempty"`
	Runtime          string   `json:"runtime,omitempty"`
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

// --- Backlog types ---

type BacklogStatus string

const (
	BacklogStatusBacklog     BacklogStatus = "backlog"
	BacklogStatusReady       BacklogStatus = "ready"
	BacklogStatusInDiscovery BacklogStatus = "in_discovery"
	BacklogStatusPlanned     BacklogStatus = "planned"
	BacklogStatusInProgress  BacklogStatus = "in_progress"
	BacklogStatusReview      BacklogStatus = "review"
	BacklogStatusBlocked     BacklogStatus = "blocked"
	BacklogStatusDone        BacklogStatus = "done"
	BacklogStatusCancelled   BacklogStatus = "cancelled"
)

type BacklogItem struct {
	ID          uuid.UUID     `json:"id"`
	Title       string        `json:"title"`
	Description string        `json:"description,omitempty"`
	ItemType    string        `json:"item_type"`
	Status      BacklogStatus `json:"status"`
	Domain      string        `json:"domain,omitempty"`
	AssignedTo  string        `json:"assigned_to,omitempty"`
	ParentID    *uuid.UUID    `json:"parent_id,omitempty"`

	// Scoring inputs
	Impact          *float64 `json:"impact,omitempty"`
	Urgency         *float64 `json:"urgency,omitempty"`
	EstimatedTokens *int64   `json:"estimated_tokens,omitempty"`
	EffortEstimate  string   `json:"effort_estimate,omitempty"`

	// Scoring outputs
	PriorityScore *float64 `json:"priority_score,omitempty"`
	ScoresSource  string   `json:"scores_source,omitempty"`

	// Model routing hints
	ModelTier  string   `json:"model_tier,omitempty"`
	Labels     []string `json:"labels,omitempty"`
	OneWayDoor bool     `json:"one_way_door"`

	// Discovery
	DiscoveryAssessment map[string]interface{} `json:"discovery_assessment,omitempty"`

	// Metadata
	Source    string                 `json:"source,omitempty"`
	Metadata map[string]interface{} `json:"metadata,omitempty"`

	// Timestamps
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BacklogFilter struct {
	Status   *BacklogStatus
	Domain   string
	AssignedTo string
	ItemType string
	ParentID *uuid.UUID
	Limit    int
	Offset   int
}

type BacklogDependency struct {
	ID         uuid.UUID  `json:"id"`
	BlockerID  uuid.UUID  `json:"blocker_id"`
	BlockedID  uuid.UUID  `json:"blocked_id"`
	ResolvedAt *time.Time `json:"resolved_at,omitempty"`
	CreatedAt  time.Time  `json:"created_at"`
}

type DispatchOverride struct {
	ID            uuid.UUID  `json:"id"`
	BacklogItemID *uuid.UUID `json:"backlog_item_id,omitempty"`
	TaskID        *uuid.UUID `json:"task_id,omitempty"`
	OverrideType  string     `json:"override_type"`
	PreviousValue string     `json:"previous_value,omitempty"`
	NewValue      string     `json:"new_value"`
	Reason        string     `json:"reason,omitempty"`
	OverriddenBy  string     `json:"overridden_by"`
	CreatedAt     time.Time  `json:"created_at"`
}

type AutonomyEvent struct {
	ID            uuid.UUID              `json:"id"`
	BacklogItemID *uuid.UUID             `json:"backlog_item_id,omitempty"`
	TaskID        *uuid.UUID             `json:"task_id,omitempty"`
	EventType     string                 `json:"event_type"`
	WasAutonomous bool                   `json:"was_autonomous"`
	Details       map[string]interface{} `json:"details,omitempty"`
	CreatedAt     time.Time              `json:"created_at"`
}

type AutonomyMetrics struct {
	Day             string  `json:"day"`
	TotalEvents     int     `json:"total_events"`
	AutonomousCount int     `json:"autonomous_count"`
	OverriddenCount int     `json:"overridden_count"`
	AutonomyRatio   float64 `json:"autonomy_ratio"`
}

// DiscoveredSubtask is a subtask discovered during the discovery phase.
type DiscoveredSubtask struct {
	Title           string   `json:"title"`
	Description     string   `json:"description,omitempty"`
	ItemType        string   `json:"item_type,omitempty"`
	Domain          string   `json:"domain,omitempty"`
	Impact          *float64 `json:"impact,omitempty"`
	Urgency         *float64 `json:"urgency,omitempty"`
	EstimatedTokens *int64   `json:"estimated_tokens,omitempty"`
	EffortEstimate  string   `json:"effort_estimate,omitempty"`
	Labels          []string `json:"labels,omitempty"`
}

// BacklogDiscoveryCompleteRequest carries the discovery assessment for a backlog item.
type BacklogDiscoveryCompleteRequest struct {
	Impact          *float64               `json:"impact,omitempty"`
	Urgency         *float64               `json:"urgency,omitempty"`
	EstimatedTokens *int64                 `json:"estimated_tokens,omitempty"`
	EffortEstimate  string                 `json:"effort_estimate,omitempty"`
	Labels          []string               `json:"labels,omitempty"`
	LabelsToRemove  []string               `json:"labels_to_remove,omitempty"`
	OneWayDoor      *bool                  `json:"one_way_door,omitempty"`
	Assessment      map[string]interface{} `json:"assessment,omitempty"`
	Park            bool                   `json:"park"`
	Subtasks        []DiscoveredSubtask    `json:"subtasks,omitempty"`
}

// DiscoveryCompleteResult is the result of a transactional discovery-complete operation.
type BacklogDiscoveryCompleteResult struct {
	Item             *BacklogItem    `json:"item"`
	PreviousScore    *float64        `json:"previous_score,omitempty"`
	UpdatedScore     *float64        `json:"updated_score,omitempty"`
	CreatedSubtasks  []*BacklogItem  `json:"created_subtasks,omitempty"`
	ModelTier        string          `json:"model_tier,omitempty"`
}

// ScoreFn computes a priority score for a backlog item given dependency info and median tokens.
type ScoreFn func(item *BacklogItem, hasUnresolvedDeps bool, medianTokens int64) float64

// TierFn derives a model tier from a backlog item.
type TierFn func(item *BacklogItem) string

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

	// Backlog
	CreateBacklogItem(ctx context.Context, item *BacklogItem) error
	GetBacklogItem(ctx context.Context, id uuid.UUID) (*BacklogItem, error)
	ListBacklogItems(ctx context.Context, filter BacklogFilter) ([]*BacklogItem, error)
	UpdateBacklogItem(ctx context.Context, item *BacklogItem) error
	DeleteBacklogItem(ctx context.Context, id uuid.UUID) error
	GetNextBacklogItems(ctx context.Context, limit int) ([]*BacklogItem, error)

	// Dependencies
	CreateDependency(ctx context.Context, dep *BacklogDependency) error
	DeleteDependency(ctx context.Context, id uuid.UUID) error
	GetDependenciesForItem(ctx context.Context, itemID uuid.UUID) ([]*BacklogDependency, error)
	HasUnresolvedBlockers(ctx context.Context, itemID uuid.UUID) (bool, error)
	ResolveDependenciesForBlocker(ctx context.Context, blockerID uuid.UUID) error

	// Overrides
	CreateOverride(ctx context.Context, o *DispatchOverride) error

	// Autonomy
	CreateAutonomyEvent(ctx context.Context, e *AutonomyEvent) error
	GetAutonomyMetrics(ctx context.Context, days int) ([]*AutonomyMetrics, error)

	// Discovery (transactional)
	BacklogDiscoveryComplete(ctx context.Context, itemID uuid.UUID, req *BacklogDiscoveryCompleteRequest, scoreFn ScoreFn, tierFn TierFn) (*BacklogDiscoveryCompleteResult, error)

	// Median tokens for scoring
	GetMedianEstimatedTokens(ctx context.Context) (int64, error)

	Close() error
}
