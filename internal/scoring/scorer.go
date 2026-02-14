package scoring

import (
	"log/slog"
)

// ScoringResult captures the complete scoring output for a single agent–task pair.
type ScoringResult struct {
	AgentSlug      string         `json:"agent_slug"`
	TotalScore     float64        `json:"total_score"`
	Factors        []FactorResult `json:"factors"`
	OversightLevel string         `json:"oversight_level"`
	FastPath       bool           `json:"fast_path"`
	Eligible       bool           `json:"eligible"`
}

// Scorer orchestrates the 11-factor weighted additive scoring engine.
type Scorer struct {
	weights         WeightSet
	fastPathEnabled bool
	logger          *slog.Logger
}

// NewScorer creates a Scorer with the given weights and configuration.
func NewScorer(weights WeightSet, fastPathEnabled bool, logger *slog.Logger) *Scorer {
	return &Scorer{
		weights:         weights,
		fastPathEnabled: fastPathEnabled,
		logger:          logger,
	}
}

// ScoreCandidate computes the full scoring result for one agent–task pair.
func (s *Scorer) ScoreCandidate(tc *TaskContext) ScoringResult {
	result := ScoringResult{
		AgentSlug: tc.Persona.Slug,
		Eligible:  true,
	}

	// Compute all 11 factors
	factors := []FactorResult{
		CapabilityFactor(tc),
		AvailabilityFactor(tc),
		RiskFitFactor(tc),
		CostEfficiencyFactor(tc),
		VerifiabilityFactor(tc),
		ReversibilityFactor(tc),
		ComplexityFitFactor(tc),
		UncertaintyFitFactor(tc),
		DurationFitFactor(tc),
		ContextualityFitFactor(tc),
		SubjectivityFitFactor(tc),
	}

	// Gate: if capability=0 or availability=0, agent is ineligible
	capScore := factors[0].Score
	availScore := factors[1].Score
	if capScore == 0 || availScore == 0 {
		result.Eligible = false
		result.TotalScore = 0
		result.Factors = factors
		return result
	}

	// Apply weights
	weights := []float64{
		s.weights.Capability,
		s.weights.Availability,
		s.weights.RiskFit,
		s.weights.CostEfficiency,
		s.weights.Verifiability,
		s.weights.Reversibility,
		s.weights.ComplexityFit,
		s.weights.UncertaintyFit,
		s.weights.DurationFit,
		s.weights.Contextuality,
		s.weights.Subjectivity,
	}

	var total float64
	for i := range factors {
		factors[i].Weight = weights[i]
		factors[i].Weighted = factors[i].Score * weights[i]
		total += factors[i].Weighted
	}

	result.TotalScore = total
	result.Factors = factors

	// Fast-path check
	if s.fastPathEnabled {
		result.FastPath = FastPathEligible(tc)
	}

	// Oversight level calculation
	result.OversightLevel = computeOversightLevel(tc)

	return result
}

// computeOversightLevel determines the required oversight based on risk, verifiability,
// reversibility, and trust.
//
//	oversightScore = risk*0.35 + (1-verifiability)*0.25 + (1-reversibility)*0.25 + (1-trust)*0.15
//
// Maps to: 0-0.2=autonomous, 0.2-0.4=notify, 0.4-0.6=review, 0.6-0.8=approve, 0.8-1.0=supervise
func computeOversightLevel(tc *TaskContext) string {
	risk := 0.5
	if tc.Task.RiskScore != nil {
		risk = *tc.Task.RiskScore
	}
	verifiability := 0.5
	if tc.Task.VerifiabilityScore != nil {
		verifiability = *tc.Task.VerifiabilityScore
	}
	reversibility := 0.5
	if tc.Task.ReversibilityScore != nil {
		reversibility = *tc.Task.ReversibilityScore
	}
	trust := 0.5
	if tc.AgentTrustLevel != nil {
		trust = *tc.AgentTrustLevel
	}

	score := risk*0.35 + (1-verifiability)*0.25 + (1-reversibility)*0.25 + (1-trust)*0.15

	switch {
	case score < 0.2:
		return "autonomous"
	case score < 0.4:
		return "notify"
	case score < 0.6:
		return "review"
	case score < 0.8:
		return "approve"
	default:
		return "supervise"
	}
}
