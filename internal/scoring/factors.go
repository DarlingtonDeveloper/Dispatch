package scoring

import (
	"math"
	"strings"

	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

// FactorResult captures one factor's contribution to the total score.
type FactorResult struct {
	Name      string  `json:"name"`
	Score     float64 `json:"score"`
	Weight    float64 `json:"weight"`
	Weighted  float64 `json:"weighted"`
	Available bool    `json:"available"`
	Reason    string  `json:"reason"`
}

// TaskContext bundles all inputs needed to score a single agent–task pair.
type TaskContext struct {
	Task            *store.Task
	Persona         forge.Persona
	AgentState      *warren.AgentState
	ActiveTaskCount int
	MaxConcurrent   int

	// Optional enrichment — nil means unavailable, factor uses default 0.5
	AgentAvgDuration *float64
	AgentAvgCost     *float64
	AgentTrustLevel  *float64
}

// --- Individual factor calculators ---

// CapabilityFactor returns 1.0 if all required capabilities are met, 0.0 otherwise.
func CapabilityFactor(tc *TaskContext) FactorResult {
	if len(tc.Task.RequiredCapabilities) == 0 {
		return FactorResult{Name: "capability", Score: 1.0, Available: true, Reason: "no capabilities required"}
	}
	for _, req := range tc.Task.RequiredCapabilities {
		found := false
		for _, cap := range tc.Persona.Capabilities {
			if strings.EqualFold(cap, req) {
				found = true
				break
			}
		}
		if !found {
			return FactorResult{Name: "capability", Score: 0.0, Available: true, Reason: "missing: " + req}
		}
	}
	return FactorResult{Name: "capability", Score: 1.0, Available: true, Reason: "all capabilities matched"}
}

// AvailabilityFactor combines agent status with task load.
func AvailabilityFactor(tc *TaskContext) FactorResult {
	switch tc.AgentState.Status {
	case "ready":
		return FactorResult{Name: "availability", Score: 1.0, Available: true, Reason: "ready"}
	case "sleeping":
		return FactorResult{Name: "availability", Score: 0.6, Available: true, Reason: "sleeping (wake penalty)"}
	case "busy":
		if tc.MaxConcurrent > 0 && tc.ActiveTaskCount >= tc.MaxConcurrent {
			return FactorResult{Name: "availability", Score: 0.0, Available: true, Reason: "at max concurrency"}
		}
		load := float64(tc.ActiveTaskCount) / float64(tc.MaxConcurrent)
		score := math.Max(0.1, 1.0-load)
		return FactorResult{Name: "availability", Score: score, Available: true, Reason: "busy with capacity"}
	default: // stopped, degraded
		return FactorResult{Name: "availability", Score: 0.0, Available: true, Reason: "unavailable: " + tc.AgentState.Status}
	}
}

// RiskFitFactor maps agent trust level vs task risk using the Shu/Ha/Ri matrix.
// trust and risk are both 0.0–1.0.
func RiskFitFactor(tc *TaskContext) FactorResult {
	trust := 0.5 // default
	if tc.AgentTrustLevel != nil {
		trust = *tc.AgentTrustLevel
	}
	risk := 0.5 // default
	if tc.Task.RiskScore != nil {
		risk = *tc.Task.RiskScore
	}

	available := tc.AgentTrustLevel != nil || tc.Task.RiskScore != nil

	// Higher trust + lower risk = better fit
	// Low trust + high risk = poor fit
	score := trust * (1.0 - risk*0.5)
	score = clamp(score, 0.0, 1.0)

	reason := "default"
	if available {
		reason = "trust/risk evaluated"
	}
	return FactorResult{Name: "risk_fit", Score: score, Available: available, Reason: reason}
}

// CostEfficiencyFactor scores based on agent's historical average cost.
// Lower cost = higher score. Without history, returns 0.5.
func CostEfficiencyFactor(tc *TaskContext) FactorResult {
	if tc.AgentAvgCost == nil {
		return FactorResult{Name: "cost_efficiency", Score: 0.5, Available: false, Reason: "no cost history"}
	}
	// Normalize: assume $1.00 is "expensive" baseline
	// Score = 1 - (cost / baseline), clamped to [0.1, 1.0]
	baseline := 1.0
	score := 1.0 - (*tc.AgentAvgCost / baseline)
	score = clamp(score, 0.1, 1.0)
	return FactorResult{Name: "cost_efficiency", Score: score, Available: true, Reason: "from history"}
}

// VerifiabilityFactor is a passthrough from task metadata.
func VerifiabilityFactor(tc *TaskContext) FactorResult {
	if tc.Task.VerifiabilityScore != nil {
		return FactorResult{Name: "verifiability", Score: clamp(*tc.Task.VerifiabilityScore, 0, 1), Available: true, Reason: "from metadata"}
	}
	return FactorResult{Name: "verifiability", Score: 0.5, Available: false, Reason: "default"}
}

// ReversibilityFactor is a passthrough from task metadata.
func ReversibilityFactor(tc *TaskContext) FactorResult {
	if tc.Task.ReversibilityScore != nil {
		return FactorResult{Name: "reversibility", Score: clamp(*tc.Task.ReversibilityScore, 0, 1), Available: true, Reason: "from metadata"}
	}
	return FactorResult{Name: "reversibility", Score: 0.5, Available: false, Reason: "default"}
}

// ComplexityFitFactor favours agents with more capabilities for complex tasks.
func ComplexityFitFactor(tc *TaskContext) FactorResult {
	complexity := 0.5
	if tc.Task.ComplexityScore != nil {
		complexity = *tc.Task.ComplexityScore
	}

	available := tc.Task.ComplexityScore != nil

	// Agent breadth: more capabilities = better fit for complex tasks
	capCount := float64(len(tc.Persona.Capabilities))
	breadth := math.Min(capCount/5.0, 1.0) // normalize: 5+ caps = 1.0

	// For complex tasks, prefer broad agents; for simple tasks, any agent works
	score := 1.0 - complexity*(1.0-breadth)
	score = clamp(score, 0.0, 1.0)

	reason := "default"
	if available {
		reason = "complexity evaluated"
	}
	return FactorResult{Name: "complexity_fit", Score: score, Available: available, Reason: reason}
}

// UncertaintyFitFactor favours agents with broader capabilities for uncertain tasks.
func UncertaintyFitFactor(tc *TaskContext) FactorResult {
	uncertainty := 0.5
	if tc.Task.UncertaintyScore != nil {
		uncertainty = *tc.Task.UncertaintyScore
	}

	available := tc.Task.UncertaintyScore != nil

	capCount := float64(len(tc.Persona.Capabilities))
	breadth := math.Min(capCount/5.0, 1.0)

	score := 1.0 - uncertainty*(1.0-breadth)
	score = clamp(score, 0.0, 1.0)

	reason := "default"
	if available {
		reason = "uncertainty evaluated"
	}
	return FactorResult{Name: "uncertainty_fit", Score: score, Available: available, Reason: reason}
}

// DurationFitFactor matches expected duration against agent reliability.
func DurationFitFactor(tc *TaskContext) FactorResult {
	if tc.AgentAvgDuration == nil {
		return FactorResult{Name: "duration_fit", Score: 0.5, Available: false, Reason: "no duration history"}
	}
	// Faster average completion = higher score
	// Normalize: 300s (5 min) baseline
	baseline := 300.0
	score := 1.0 - (*tc.AgentAvgDuration / baseline)
	score = clamp(score, 0.1, 1.0)
	return FactorResult{Name: "duration_fit", Score: score, Available: true, Reason: "from history"}
}

// ContextualityFitFactor is a passthrough from task metadata.
func ContextualityFitFactor(tc *TaskContext) FactorResult {
	if tc.Task.ContextualityScore != nil {
		return FactorResult{Name: "contextuality", Score: clamp(*tc.Task.ContextualityScore, 0, 1), Available: true, Reason: "from metadata"}
	}
	return FactorResult{Name: "contextuality", Score: 0.5, Available: false, Reason: "default"}
}

// SubjectivityFitFactor is a passthrough from task metadata.
func SubjectivityFitFactor(tc *TaskContext) FactorResult {
	if tc.Task.SubjectivityScore != nil {
		return FactorResult{Name: "subjectivity", Score: clamp(*tc.Task.SubjectivityScore, 0, 1), Available: true, Reason: "from metadata"}
	}
	return FactorResult{Name: "subjectivity", Score: 0.5, Available: false, Reason: "default"}
}

func clamp(v, min, max float64) float64 {
	if v < min {
		return min
	}
	if v > max {
		return max
	}
	return v
}
