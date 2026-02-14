package scoring

import (
	"io"
	"log/slog"
	"math"
	"testing"

	"github.com/MikeSquared-Agency/Dispatch/internal/forge"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
	"github.com/MikeSquared-Agency/Dispatch/internal/warren"
)

func discardLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func float64Ptr(v float64) *float64 { return &v }

func TestDefaultWeightsSumToOne(t *testing.T) {
	w := DefaultWeights()
	if err := w.Validate(); err != nil {
		t.Errorf("default weights invalid: %v", err)
	}
	if math.Abs(w.Sum()-1.0) > 0.001 {
		t.Errorf("default weights sum to %f, expected 1.0", w.Sum())
	}
}

func TestCapabilityFactor(t *testing.T) {
	t.Run("match", func(t *testing.T) {
		tc := &TaskContext{
			Task:    &store.Task{RequiredCapabilities: []string{"research"}},
			Persona: forge.Persona{Capabilities: []string{"research", "analysis"}},
		}
		r := CapabilityFactor(tc)
		if r.Score != 1.0 {
			t.Errorf("expected 1.0, got %f", r.Score)
		}
		if !r.Available {
			t.Error("expected available=true")
		}
	})

	t.Run("mismatch", func(t *testing.T) {
		tc := &TaskContext{
			Task:    &store.Task{RequiredCapabilities: []string{"code"}},
			Persona: forge.Persona{Capabilities: []string{"research"}},
		}
		r := CapabilityFactor(tc)
		if r.Score != 0.0 {
			t.Errorf("expected 0.0, got %f", r.Score)
		}
	})

	t.Run("no requirements", func(t *testing.T) {
		tc := &TaskContext{
			Task:    &store.Task{},
			Persona: forge.Persona{Capabilities: []string{"research"}},
		}
		r := CapabilityFactor(tc)
		if r.Score != 1.0 {
			t.Errorf("expected 1.0 for no requirements, got %f", r.Score)
		}
	})
}

func TestAvailabilityFactor(t *testing.T) {
	tests := []struct {
		name   string
		status string
		active int
		max    int
		want   float64
	}{
		{"ready", "ready", 0, 3, 1.0},
		{"sleeping", "sleeping", 0, 3, 0.6},
		{"busy with capacity", "busy", 1, 3, 0.6667},
		{"busy at max", "busy", 3, 3, 0.0},
		{"stopped", "stopped", 0, 3, 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &TaskContext{
				Task:            &store.Task{},
				Persona:         forge.Persona{},
				AgentState:      &warren.AgentState{Status: tt.status},
				ActiveTaskCount: tt.active,
				MaxConcurrent:   tt.max,
			}
			r := AvailabilityFactor(tc)
			if math.Abs(r.Score-tt.want) > 0.01 {
				t.Errorf("got %f, want %f", r.Score, tt.want)
			}
		})
	}
}

func TestFastPathEligible(t *testing.T) {
	t.Run("positive", func(t *testing.T) {
		tc := &TaskContext{
			Task: &store.Task{
				ComplexityScore:    float64Ptr(0.1),
				RiskScore:          float64Ptr(0.1),
				ReversibilityScore: float64Ptr(0.9),
			},
		}
		if !FastPathEligible(tc) {
			t.Error("expected fast path eligible")
		}
	})

	t.Run("negative high risk", func(t *testing.T) {
		tc := &TaskContext{
			Task: &store.Task{
				ComplexityScore:    float64Ptr(0.1),
				RiskScore:          float64Ptr(0.5),
				ReversibilityScore: float64Ptr(0.9),
			},
		}
		if FastPathEligible(tc) {
			t.Error("expected NOT fast path eligible with high risk")
		}
	})

	t.Run("defaults not eligible", func(t *testing.T) {
		tc := &TaskContext{
			Task: &store.Task{},
		}
		if FastPathEligible(tc) {
			t.Error("all-defaults should NOT trigger fast path")
		}
	})
}

func TestScoreCandidateFullContext(t *testing.T) {
	s := NewScorer(DefaultWeights(), true, discardLogger())
	tc := &TaskContext{
		Task: &store.Task{
			RequiredCapabilities: []string{"research"},
			RiskScore:            float64Ptr(0.3),
			VerifiabilityScore:   float64Ptr(0.8),
			ReversibilityScore:   float64Ptr(0.7),
			ComplexityScore:      float64Ptr(0.4),
			UncertaintyScore:     float64Ptr(0.3),
		},
		Persona:         forge.Persona{Slug: "lily", Capabilities: []string{"research", "analysis"}},
		AgentState:      &warren.AgentState{Name: "lily", Status: "ready"},
		ActiveTaskCount: 0,
		MaxConcurrent:   3,
		AgentAvgDuration: float64Ptr(120.0),
		AgentAvgCost:     float64Ptr(0.05),
		AgentTrustLevel:  float64Ptr(0.8),
	}

	result := s.ScoreCandidate(tc)
	if !result.Eligible {
		t.Fatal("expected eligible")
	}
	if result.TotalScore <= 0 {
		t.Errorf("expected positive score, got %f", result.TotalScore)
	}
	if len(result.Factors) != 11 {
		t.Errorf("expected 11 factors, got %d", len(result.Factors))
	}
	if result.OversightLevel == "" {
		t.Error("expected oversight level set")
	}
}

func TestScoreCandidateMinimalContext(t *testing.T) {
	s := NewScorer(DefaultWeights(), true, discardLogger())
	tc := &TaskContext{
		Task:            &store.Task{RequiredCapabilities: []string{"research"}},
		Persona:         forge.Persona{Slug: "lily", Capabilities: []string{"research"}},
		AgentState:      &warren.AgentState{Name: "lily", Status: "ready"},
		ActiveTaskCount: 0,
		MaxConcurrent:   3,
	}

	result := s.ScoreCandidate(tc)
	if !result.Eligible {
		t.Fatal("expected eligible with minimal context")
	}
	if result.TotalScore <= 0 {
		t.Errorf("expected positive score, got %f", result.TotalScore)
	}
	// With defaults, most factors return 0.5
	// capability=1.0, availability=1.0, rest=0.5
	// Score should be 0.20*1.0 + 0.10*1.0 + 0.62*0.5 (simplified) ≈ 0.61
	if result.TotalScore < 0.5 || result.TotalScore > 0.9 {
		t.Errorf("expected score in [0.5, 0.9], got %f", result.TotalScore)
	}
}

func TestScoreCandidateIneligible(t *testing.T) {
	s := NewScorer(DefaultWeights(), true, discardLogger())

	t.Run("missing capability", func(t *testing.T) {
		tc := &TaskContext{
			Task:       &store.Task{RequiredCapabilities: []string{"code"}},
			Persona:    forge.Persona{Slug: "lily", Capabilities: []string{"research"}},
			AgentState: &warren.AgentState{Status: "ready"},
		}
		result := s.ScoreCandidate(tc)
		if result.Eligible {
			t.Error("expected ineligible for missing capability")
		}
		if result.TotalScore != 0 {
			t.Errorf("expected 0 score, got %f", result.TotalScore)
		}
	})

	t.Run("stopped agent", func(t *testing.T) {
		tc := &TaskContext{
			Task:       &store.Task{RequiredCapabilities: []string{"research"}},
			Persona:    forge.Persona{Slug: "lily", Capabilities: []string{"research"}},
			AgentState: &warren.AgentState{Status: "stopped"},
		}
		result := s.ScoreCandidate(tc)
		if result.Eligible {
			t.Error("expected ineligible for stopped agent")
		}
	})
}

func TestOversightLevels(t *testing.T) {
	tests := []struct {
		name           string
		risk           float64
		verifiability  float64
		reversibility  float64
		trust          float64
		expectedLevel  string
	}{
		{"low risk high trust", 0.0, 1.0, 1.0, 1.0, "autonomous"},
		{"moderate", 0.5, 0.5, 0.5, 0.5, "review"},
		{"high risk low trust", 0.9, 0.1, 0.1, 0.1, "supervise"},
		{"medium risk", 0.4, 0.6, 0.6, 0.7, "notify"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tc := &TaskContext{
				Task: &store.Task{
					RiskScore:          &tt.risk,
					VerifiabilityScore: &tt.verifiability,
					ReversibilityScore: &tt.reversibility,
				},
				AgentTrustLevel: &tt.trust,
			}
			level := computeOversightLevel(tc)
			if level != tt.expectedLevel {
				// Compute the actual score for debugging
				score := tt.risk*0.35 + (1-tt.verifiability)*0.25 + (1-tt.reversibility)*0.25 + (1-tt.trust)*0.15
				t.Errorf("expected %s, got %s (score=%.3f)", tt.expectedLevel, level, score)
			}
		})
	}
}

func TestParetoFrontier(t *testing.T) {
	candidates := []ParetoCandidate{
		{AgentSlug: "a", Speed: 0.9, Cost: 0.8, Quality: 0.7, Risk: 0.2},
		{AgentSlug: "b", Speed: 0.7, Cost: 0.9, Quality: 0.8, Risk: 0.3},
		{AgentSlug: "c", Speed: 0.5, Cost: 0.5, Quality: 0.5, Risk: 0.5}, // dominated by a and b
	}

	frontier := ComputeFrontier(candidates)

	// c should be dominated — a and b should remain
	if len(frontier) != 2 {
		t.Errorf("expected 2 frontier members, got %d", len(frontier))
		for _, f := range frontier {
			t.Logf("  %s", f.AgentSlug)
		}
	}

	slugs := make(map[string]bool)
	for _, f := range frontier {
		slugs[f.AgentSlug] = true
	}
	if !slugs["a"] || !slugs["b"] {
		t.Error("expected both a and b on frontier")
	}
	if slugs["c"] {
		t.Error("c should be dominated")
	}
}

func TestParetoFrontierSingleCandidate(t *testing.T) {
	candidates := []ParetoCandidate{
		{AgentSlug: "only", Speed: 0.5, Cost: 0.5, Quality: 0.5, Risk: 0.5},
	}
	frontier := ComputeFrontier(candidates)
	if len(frontier) != 1 {
		t.Errorf("expected 1 frontier member, got %d", len(frontier))
	}
}
