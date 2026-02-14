package scoring

// FastPathEligible returns true if a task is simple enough to skip full scoring
// and be dispatched immediately. With all-defaults (0.5), this does NOT trigger.
func FastPathEligible(tc *TaskContext) bool {
	complexity := 0.5
	if tc.Task.ComplexityScore != nil {
		complexity = *tc.Task.ComplexityScore
	}
	risk := 0.5
	if tc.Task.RiskScore != nil {
		risk = *tc.Task.RiskScore
	}
	reversibility := 0.5
	if tc.Task.ReversibilityScore != nil {
		reversibility = *tc.Task.ReversibilityScore
	}

	return complexity < 0.2 && risk < 0.3 && reversibility > 0.7
}
