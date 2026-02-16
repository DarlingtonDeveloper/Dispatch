package scoring

import (
	"math"
	"testing"

	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

func TestDefaultBacklogWeightsSum(t *testing.T) {
	w := DefaultBacklogWeights()
	if err := w.Validate(); err != nil {
		t.Fatalf("default backlog weights invalid: %v", err)
	}
	if math.Abs(w.Sum()-1.0) > 0.001 {
		t.Fatalf("expected sum 1.0, got %f", w.Sum())
	}
}

func TestBacklogWeightsValidateNegative(t *testing.T) {
	w := BacklogWeightSet{BusinessImpact: -0.1, DependencyReadiness: 0.4, Urgency: 0.4, CostEfficiency: 0.3}
	if err := w.Validate(); err == nil {
		t.Fatal("expected validation error for negative weight")
	}
}

func TestBacklogWeightsValidateBadSum(t *testing.T) {
	w := BacklogWeightSet{BusinessImpact: 0.5, DependencyReadiness: 0.5, Urgency: 0.5, CostEfficiency: 0.5}
	if err := w.Validate(); err == nil {
		t.Fatal("expected validation error for bad sum")
	}
}

func TestBacklogScorerBusinessImpact(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	impact := 0.8
	ctx := &BacklogScoringContext{
		Item: &store.BacklogItem{Impact: &impact},
	}
	result := scorer.Score(ctx)

	// Find business_impact factor
	var found bool
	for _, f := range result.Factors {
		if f.Name == "business_impact" {
			found = true
			if f.Score != 0.8 {
				t.Errorf("expected business_impact score 0.8, got %f", f.Score)
			}
			if !f.Available {
				t.Error("expected business_impact to be available")
			}
		}
	}
	if !found {
		t.Error("business_impact factor not found")
	}
}

func TestBacklogScorerDependencyReadiness(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())

	// No deps
	ctx := &BacklogScoringContext{
		Item:              &store.BacklogItem{},
		HasUnresolvedDeps: false,
	}
	result := scorer.Score(ctx)
	for _, f := range result.Factors {
		if f.Name == "dependency_readiness" && f.Score != 1.0 {
			t.Errorf("expected dep readiness 1.0 (no deps), got %f", f.Score)
		}
	}

	// With deps
	ctx.HasUnresolvedDeps = true
	result = scorer.Score(ctx)
	for _, f := range result.Factors {
		if f.Name == "dependency_readiness" && f.Score != 0.0 {
			t.Errorf("expected dep readiness 0.0 (blocked), got %f", f.Score)
		}
	}
}

func TestBacklogScorerUrgency(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	urgency := 0.95
	ctx := &BacklogScoringContext{
		Item: &store.BacklogItem{Urgency: &urgency},
	}
	result := scorer.Score(ctx)
	for _, f := range result.Factors {
		if f.Name == "urgency" && f.Score != 0.95 {
			t.Errorf("expected urgency score 0.95, got %f", f.Score)
		}
	}
}

func TestBacklogScorerCostEfficiency(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	tokens := int64(5000)
	ctx := &BacklogScoringContext{
		Item:         &store.BacklogItem{EstimatedTokens: &tokens},
		MedianTokens: 10000,
	}
	result := scorer.Score(ctx)
	for _, f := range result.Factors {
		if f.Name == "cost_efficiency" {
			// 1.0 - (5000/10000) = 0.5
			if math.Abs(f.Score-0.5) > 0.001 {
				t.Errorf("expected cost_efficiency 0.5, got %f", f.Score)
			}
		}
	}
}

func TestBacklogScorerCostEfficiencyNoEstimate(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	ctx := &BacklogScoringContext{
		Item:         &store.BacklogItem{},
		MedianTokens: 10000,
	}
	result := scorer.Score(ctx)
	for _, f := range result.Factors {
		if f.Name == "cost_efficiency" && f.Score != 0.5 {
			t.Errorf("expected cost_efficiency default 0.5, got %f", f.Score)
		}
	}
}

func TestBacklogScorerFullScore(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	impact := 1.0
	urgency := 1.0
	tokens := int64(0) // very cheap
	ctx := &BacklogScoringContext{
		Item: &store.BacklogItem{
			Impact:          &impact,
			Urgency:         &urgency,
			EstimatedTokens: &tokens,
		},
		HasUnresolvedDeps: false,
		MedianTokens:      10000,
	}
	result := scorer.Score(ctx)
	// Max score: impact(1.0)*0.30 + dep(1.0)*0.25 + urgency(1.0)*0.25 + cost(1.0)*0.20 = 1.0
	if math.Abs(result.TotalScore-1.0) > 0.001 {
		t.Errorf("expected max total score 1.0, got %f", result.TotalScore)
	}
}

func TestBacklogScorerBlockedItemScore(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	impact := 1.0
	urgency := 1.0
	ctx := &BacklogScoringContext{
		Item: &store.BacklogItem{
			Impact:  &impact,
			Urgency: &urgency,
		},
		HasUnresolvedDeps: true,
		MedianTokens:      0,
	}
	result := scorer.Score(ctx)
	// dep readiness = 0, so total = 1.0*0.30 + 0.0*0.25 + 1.0*0.25 + 0.5*0.20 = 0.65
	expected := 0.65
	if math.Abs(result.TotalScore-expected) > 0.001 {
		t.Errorf("expected blocked total score %.2f, got %f", expected, result.TotalScore)
	}
}

func TestScoreItemCallback(t *testing.T) {
	scorer := NewBacklogScorer(DefaultBacklogWeights())
	impact := 0.7
	urgency := 0.5
	item := &store.BacklogItem{
		Impact:  &impact,
		Urgency: &urgency,
	}
	score := scorer.ScoreItem(item, false, 0)
	if score <= 0 || score > 1.0 {
		t.Errorf("expected score in (0, 1.0], got %f", score)
	}
}
