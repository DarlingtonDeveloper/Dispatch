package hermes

import "time"

type TaskRequestEvent struct {
	Owner                string                 `json:"owner"`
	Title                string                 `json:"title"`
	Description          string                 `json:"description,omitempty"`
	RequiredCapabilities []string               `json:"required_capabilities,omitempty"`
	Priority             int                    `json:"priority,omitempty"`
	Metadata             map[string]interface{} `json:"metadata,omitempty"`
	TimeoutSeconds       int                    `json:"timeout_seconds,omitempty"`
	MaxRetries           int                    `json:"max_retries,omitempty"`
	Source               string                 `json:"source,omitempty"`
}

type TaskAssignedEvent struct {
	TaskID        string `json:"task_id"`
	AssignedAgent string `json:"assigned_agent"`
}

type TaskCompletedEvent struct {
	TaskID string                 `json:"task_id"`
	Result map[string]interface{} `json:"result,omitempty"`
}

type TaskFailedEvent struct {
	TaskID        string `json:"task_id"`
	Error         string `json:"error"`
	RetryEligible bool   `json:"retry_eligible"`
}

type TaskTimeoutEvent struct {
	TaskID     string `json:"task_id"`
	RetryCount int    `json:"retry_count"`
	MaxRetries int    `json:"max_retries"`
	TimedOutIn string `json:"timed_out_in_state"`
}

type StatsEvent struct {
	Pending    int       `json:"pending"`
	InProgress int       `json:"in_progress"`
	Completed  int       `json:"completed"`
	Failed     int       `json:"failed"`
	AvgMs      float64   `json:"avg_completion_ms"`
	Timestamp  time.Time `json:"timestamp"`
}

// DispatchAssignedEvent carries the full v2 scoring breakdown for an assignment.
type DispatchAssignedEvent struct {
	TaskID           string      `json:"task_id"`
	AssignedAgent    string      `json:"assigned_agent"`
	TotalScore       float64     `json:"total_score"`
	Factors          interface{} `json:"factors"`
	OversightLevel   string      `json:"oversight_level"`
	FastPath         bool        `json:"fast_path"`
	RecommendedModel string      `json:"recommended_model,omitempty"`
	ModelTier        string      `json:"model_tier,omitempty"`
	RoutingMethod    string      `json:"routing_method,omitempty"`
	Runtime          string      `json:"runtime,omitempty"`
}

// DispatchCompletedEvent carries actuals recorded on task completion.
type DispatchCompletedEvent struct {
	TaskID          string  `json:"task_id"`
	Agent           string  `json:"agent"`
	DurationSeconds float64 `json:"duration_seconds"`
	TokensUsed      int64   `json:"tokens_used,omitempty"`
	CostUSD         float64 `json:"cost_usd,omitempty"`
}

// OversightSetEvent carries the oversight level and MC gate config for a task.
type OversightSetEvent struct {
	TaskID         string `json:"task_id"`
	OversightLevel string `json:"oversight_level"`
}

// BacklogItemEvent carries a backlog item lifecycle transition.
type BacklogItemEvent struct {
	ItemID string `json:"item_id"`
	Status string `json:"status"`
	Title  string `json:"title"`
}

// BacklogDiscoveryCompleteEvent carries the results of a discovery-complete transition.
type BacklogDiscoveryCompleteEvent struct {
	ItemID        string   `json:"item_id"`
	Status        string   `json:"status"`
	PreviousScore *float64 `json:"previous_score,omitempty"`
	UpdatedScore  *float64 `json:"updated_score,omitempty"`
	ModelTier     string   `json:"model_tier,omitempty"`
	SubtaskCount  int      `json:"subtask_count"`
}

// OverrideRecordedEvent carries a human override event.
type OverrideRecordedEvent struct {
	OverrideID   string `json:"override_id"`
	OverrideType string `json:"override_type"`
	OverriddenBy string `json:"overridden_by"`
	NewValue     string `json:"new_value"`
}
