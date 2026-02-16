package scoring

import (
	"fmt"
	"math"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

// BacklogWeightSet defines the 4-factor weight distribution for backlog item scoring.
type BacklogWeightSet struct {
	BusinessImpact      float64
	DependencyReadiness float64
	Urgency             float64
	CostEfficiency      float64
}

// DefaultBacklogWeights returns the spec-defined weight distribution.
func DefaultBacklogWeights() BacklogWeightSet {
	return BacklogWeightSet{
		BusinessImpact:      0.30,
		DependencyReadiness: 0.25,
		Urgency:             0.25,
		CostEfficiency:      0.20,
	}
}

// Sum returns the total of all weights.
func (w BacklogWeightSet) Sum() float64 {
	return w.BusinessImpact + w.DependencyReadiness + w.Urgency + w.CostEfficiency
}

// Validate checks that weights sum to 1.0 and none are negative.
func (w BacklogWeightSet) Validate() error {
	if math.Abs(w.Sum()-1.0) > 0.001 {
		return fmt.Errorf("backlog weights sum to %.4f, must sum to 1.0", w.Sum())
	}
	for _, v := range []float64{w.BusinessImpact, w.DependencyReadiness, w.Urgency, w.CostEfficiency} {
		if v < 0 {
			return fmt.Errorf("negative backlog weight: %f", v)
		}
	}
	return nil
}

// BacklogScoringContext bundles all inputs needed to score a single backlog item.
type BacklogScoringContext struct {
	Item              *store.BacklogItem
	HasUnresolvedDeps bool
	MedianTokens      int64
}

// BacklogScoringResult captures the scoring output for a single backlog item.
type BacklogScoringResult struct {
	TotalScore float64        `json:"total_score"`
	Factors    []FactorResult `json:"factors"`
}

// BacklogScorer scores backlog items using 4 weighted factors.
type BacklogScorer struct {
	weights BacklogWeightSet
}

// NewBacklogScorer creates a BacklogScorer with the given weights.
func NewBacklogScorer(weights BacklogWeightSet) *BacklogScorer {
	return &BacklogScorer{weights: weights}
}

// Score computes the 4-factor priority score for a backlog item.
func (s *BacklogScorer) Score(ctx *BacklogScoringContext) BacklogScoringResult {
	factors := []FactorResult{
		s.businessImpact(ctx),
		s.dependencyReadiness(ctx),
		s.urgency(ctx),
		s.costEfficiency(ctx),
	}

	weights := []float64{
		s.weights.BusinessImpact,
		s.weights.DependencyReadiness,
		s.weights.Urgency,
		s.weights.CostEfficiency,
	}

	var total float64
	for i := range factors {
		factors[i].Weight = weights[i]
		factors[i].Weighted = factors[i].Score * weights[i]
		total += factors[i].Weighted
	}

	return BacklogScoringResult{
		TotalScore: total,
		Factors:    factors,
	}
}

// ScoreItem is a callback-compatible helper for use with store.BacklogDiscoveryComplete.
func (s *BacklogScorer) ScoreItem(item *store.BacklogItem, hasUnresolvedDeps bool, medianTokens int64) float64 {
	ctx := &BacklogScoringContext{
		Item:              item,
		HasUnresolvedDeps: hasUnresolvedDeps,
		MedianTokens:      medianTokens,
	}
	result := s.Score(ctx)
	return result.TotalScore
}

// --- Factor calculators ---

// businessImpact is a passthrough from item.Impact.
func (s *BacklogScorer) businessImpact(ctx *BacklogScoringContext) FactorResult {
	if ctx.Item.Impact != nil {
		return FactorResult{
			Name:      "business_impact",
			Score:     clamp(*ctx.Item.Impact, 0, 1),
			Available: true,
			Reason:    "from item impact",
		}
	}
	return FactorResult{
		Name:      "business_impact",
		Score:     0.5,
		Available: false,
		Reason:    "default",
	}
}

// dependencyReadiness returns 1.0 if no unresolved deps, 0.0 if blocked.
func (s *BacklogScorer) dependencyReadiness(ctx *BacklogScoringContext) FactorResult {
	if ctx.HasUnresolvedDeps {
		return FactorResult{
			Name:      "dependency_readiness",
			Score:     0.0,
			Available: true,
			Reason:    "has unresolved blockers",
		}
	}
	return FactorResult{
		Name:      "dependency_readiness",
		Score:     1.0,
		Available: true,
		Reason:    "no blockers",
	}
}

// urgency is a passthrough from item.Urgency.
func (s *BacklogScorer) urgency(ctx *BacklogScoringContext) FactorResult {
	if ctx.Item.Urgency != nil {
		return FactorResult{
			Name:      "urgency",
			Score:     clamp(*ctx.Item.Urgency, 0, 1),
			Available: true,
			Reason:    "from item urgency",
		}
	}
	return FactorResult{
		Name:      "urgency",
		Score:     0.5,
		Available: false,
		Reason:    "default",
	}
}

// costEfficiency scores based on estimated token cost relative to median.
// Lower cost = higher score. 1.0 - (estimated / median), clamped [0, 1], default 0.5.
func (s *BacklogScorer) costEfficiency(ctx *BacklogScoringContext) FactorResult {
	if ctx.Item.EstimatedTokens == nil || ctx.MedianTokens <= 0 {
		return FactorResult{
			Name:      "cost_efficiency",
			Score:     0.5,
			Available: false,
			Reason:    "no token estimate or median",
		}
	}

	ratio := float64(*ctx.Item.EstimatedTokens) / float64(ctx.MedianTokens)
	score := clamp(1.0-ratio, 0.0, 1.0)

	return FactorResult{
		Name:      "cost_efficiency",
		Score:     score,
		Available: true,
		Reason:    "from token estimate",
	}
}
