package scoring

import (
	"testing"

	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

func testConfig() config.ModelRoutingConfig {
	return config.ModelRoutingConfig{
		Enabled:     true,
		DefaultTier: "standard",
		ColdStartRules: []config.ColdStartRule{
			{Name: "config-only", Labels: []string{"config"}, FilePatterns: []string{"*.yaml", "*.yml", "*.toml", "*.json", "*.env"}, Tier: "economy"},
			{Name: "single-file-lint", Labels: []string{"lint", "format"}, MaxFiles: 1, Tier: "economy"},
			{Name: "architecture", Labels: []string{"architecture", "design", "refactor"}, Tier: "premium"},
		},
		Tiers: []config.ModelTierDef{
			{Name: "economy", Models: []string{"claude-haiku-4-5-20251001"}},
			{Name: "standard", Models: []string{"claude-sonnet-4-5-20250929"}},
			{Name: "premium", Models: []string{"claude-opus-4-6"}},
		},
	}
}

func TestDeriveModelTier_ColdStart_ConfigOnly(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels:       []string{"config"},
		FilePatterns: []string{"app.yaml"},
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "economy" {
		t.Errorf("expected economy, got %s", tier.Name)
	}
	if tier.RoutingMethod != "cold_start" {
		t.Errorf("expected routing_method cold_start, got %s", tier.RoutingMethod)
	}
}

func TestDeriveModelTier_ColdStart_SingleFileLint(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels:       []string{"lint"},
		FilePatterns: []string{"main.go"},
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "economy" {
		t.Errorf("expected economy, got %s", tier.Name)
	}
}

func TestDeriveModelTier_ColdStart_Architecture(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels: []string{"architecture"},
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "premium" {
		t.Errorf("expected premium, got %s", tier.Name)
	}
}

func TestDeriveModelTier_ColdStart_NoMatch(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels: []string{"testing"},
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "standard" {
		t.Errorf("expected standard (default), got %s", tier.Name)
	}
}

func TestDeriveModelTier_OneWayDoor_AlwaysPremium(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels:     []string{"config"},
		OneWayDoor: true,
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "premium" {
		t.Errorf("one_way_door should force premium, got %s", tier.Name)
	}
}

func TestDeriveModelTier_HighRisk_AlwaysPremium(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels:    []string{"config"},
		RiskScore: float64Ptr(0.9),
	}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "premium" {
		t.Errorf("high risk should force premium, got %s", tier.Name)
	}
}

func TestDeriveModelTier_Disabled(t *testing.T) {
	cfg := testConfig()
	cfg.Enabled = false
	task := &store.Task{Labels: []string{"architecture"}}
	tier := DeriveModelTier(task, cfg, false)
	if tier.Name != "standard" {
		t.Errorf("disabled routing should return default tier, got %s", tier.Name)
	}
}

func TestDeriveModelTier_LearnedData_LowComplexity(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		ComplexityScore:    float64Ptr(0.1),
		RiskScore:          float64Ptr(0.1),
		ReversibilityScore: float64Ptr(0.9),
	}
	tier := DeriveModelTier(task, cfg, true)
	if tier.Name != "economy" {
		t.Errorf("low complexity+risk should be economy, got %s", tier.Name)
	}
	if tier.RoutingMethod != "learned" {
		t.Errorf("expected routing_method learned, got %s", tier.RoutingMethod)
	}
}

func TestDeriveModelTier_LearnedData_HighComplexity(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		ComplexityScore:    float64Ptr(0.9),
		RiskScore:          float64Ptr(0.7),
		ReversibilityScore: float64Ptr(0.1),
	}
	tier := DeriveModelTier(task, cfg, true)
	if tier.Name != "premium" {
		t.Errorf("high complexity+risk should be premium, got %s", tier.Name)
	}
}

func TestDeriveModelTier_LearnedData_MidRange(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		ComplexityScore:    float64Ptr(0.5),
		RiskScore:          float64Ptr(0.4),
		ReversibilityScore: float64Ptr(0.5),
	}
	tier := DeriveModelTier(task, cfg, true)
	if tier.Name != "standard" {
		t.Errorf("mid-range scores should be standard, got %s", tier.Name)
	}
}

func TestColdStartRoute_FilePatternGlob(t *testing.T) {
	rules := []config.ColdStartRule{
		{Name: "config-only", Labels: []string{"config"}, FilePatterns: []string{"*.yaml"}, Tier: "economy"},
	}
	task := &store.Task{
		Labels:       []string{"config"},
		FilePatterns: []string{"deploy.yaml"},
	}
	result := ColdStartRoute(task, rules)
	if result == nil {
		t.Fatal("expected match")
	}
	if result.Name != "economy" {
		t.Errorf("expected economy, got %s", result.Name)
	}
}

func TestColdStartRoute_FilePatternNoMatch(t *testing.T) {
	rules := []config.ColdStartRule{
		{Name: "config-only", Labels: []string{"config"}, FilePatterns: []string{"*.yaml"}, Tier: "economy"},
	}
	task := &store.Task{
		Labels:       []string{"config"},
		FilePatterns: []string{"main.go"},
	}
	result := ColdStartRoute(task, rules)
	if result != nil {
		t.Errorf("expected no match for .go file against *.yaml rule, got %s", result.Name)
	}
}

func TestColdStartRoute_MaxFilesExceeded(t *testing.T) {
	rules := []config.ColdStartRule{
		{Name: "single-file", Labels: []string{"lint"}, MaxFiles: 1, Tier: "economy"},
	}
	task := &store.Task{
		Labels:       []string{"lint"},
		FilePatterns: []string{"a.go", "b.go"},
	}
	result := ColdStartRoute(task, rules)
	if result != nil {
		t.Errorf("expected no match when max_files exceeded, got %s", result.Name)
	}
}

func TestMatchesColdStartRule_LabelRequired(t *testing.T) {
	rule := config.ColdStartRule{Labels: []string{"deploy"}, Tier: "economy"}
	task := &store.Task{Labels: []string{"testing"}}
	if matchesColdStartRule(task, rule) {
		t.Error("should not match without matching label")
	}
}

func TestRuntimeForTier(t *testing.T) {
	tests := []struct {
		tier      string
		fileCount int
		want      string
	}{
		{"economy", 0, "picoclaw"},
		{"economy", 5, "picoclaw"},
		{"standard", 0, "picoclaw"},
		{"standard", 1, "picoclaw"},
		{"standard", 2, "openclaw"},
		{"premium", 0, "openclaw"},
		{"premium", 1, "openclaw"},
		{"unknown", 0, "openclaw"},
	}
	for _, tt := range tests {
		got := RuntimeForTier(tt.tier, tt.fileCount)
		if got != tt.want {
			t.Errorf("RuntimeForTier(%q, %d) = %q, want %q", tt.tier, tt.fileCount, got, tt.want)
		}
	}
}

func TestDeriveModelTier_RoutingMethod_LearnedWithOverride(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{
		Labels:     []string{"config"},
		OneWayDoor: true,
	}
	// Even with hasLearnedData=true, one-way-door forces premium but routing method should be "learned"
	tier := DeriveModelTier(task, cfg, true)
	if tier.Name != "premium" {
		t.Errorf("expected premium, got %s", tier.Name)
	}
	if tier.RoutingMethod != "learned" {
		t.Errorf("expected routing_method learned (hasLearnedData=true), got %s", tier.RoutingMethod)
	}
}

func TestDeriveModelTier_RoutingMethod_ColdStartDefault(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{Labels: []string{"testing"}} // no rule match → default tier
	tier := DeriveModelTier(task, cfg, false)
	if tier.RoutingMethod != "cold_start" {
		t.Errorf("expected routing_method cold_start, got %s", tier.RoutingMethod)
	}
}

func TestDeriveModelTier_TierModels(t *testing.T) {
	cfg := testConfig()
	task := &store.Task{Labels: []string{"architecture"}}
	tier := DeriveModelTier(task, cfg, false)
	if len(tier.Models) == 0 {
		t.Error("expected models to be populated")
	}
	if tier.Models[0] != "claude-opus-4-6" {
		t.Errorf("expected opus model, got %s", tier.Models[0])
	}
}

// --- Effectiveness Safety Net Tests ---

func TestApplyEffectivenessSafetyNet_EconomyPromotedToStandard(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "economy", Models: []string{"claude-haiku-4-5-20251001"}, RoutingMethod: "cold_start"}
	effectiveness := map[string]EffectivenessStats{
		"economy": {CorrectionRate: 0.65, AvgEffectiveness: 0.60, SessionCount: 50},
	}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if !result.Promoted {
		t.Fatal("expected promotion")
	}
	if promoted.Name != "standard" {
		t.Errorf("expected standard, got %s", promoted.Name)
	}
	if promoted.RoutingMethod != "cold_start" {
		t.Errorf("expected routing method preserved as cold_start, got %s", promoted.RoutingMethod)
	}
	if result.OriginalTier != "economy" {
		t.Errorf("expected original tier economy, got %s", result.OriginalTier)
	}
}

func TestApplyEffectivenessSafetyNet_EconomyNotPromoted(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "economy", Models: []string{"claude-haiku-4-5-20251001"}, RoutingMethod: "cold_start"}
	effectiveness := map[string]EffectivenessStats{
		"economy": {CorrectionRate: 0.15, AvgEffectiveness: 0.72, SessionCount: 45},
	}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("should not promote when correction rate is low")
	}
	if promoted.Name != "economy" {
		t.Errorf("expected economy, got %s", promoted.Name)
	}
}

func TestApplyEffectivenessSafetyNet_StandardPromotedToPremium(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "standard", Models: []string{"claude-sonnet-4-5-20250929"}, RoutingMethod: "learned"}
	effectiveness := map[string]EffectivenessStats{
		"standard": {CorrectionRate: 0.85, AvgEffectiveness: 0.50, SessionCount: 100},
	}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if !result.Promoted {
		t.Fatal("expected promotion")
	}
	if promoted.Name != "premium" {
		t.Errorf("expected premium, got %s", promoted.Name)
	}
	if promoted.RoutingMethod != "learned" {
		t.Errorf("expected routing method preserved as learned, got %s", promoted.RoutingMethod)
	}
}

func TestApplyEffectivenessSafetyNet_StandardNotPromoted(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "standard", Models: []string{"claude-sonnet-4-5-20250929"}, RoutingMethod: "cold_start"}
	effectiveness := map[string]EffectivenessStats{
		"standard": {CorrectionRate: 0.08, AvgEffectiveness: 0.85, SessionCount: 120},
	}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("should not promote when correction rate is within threshold")
	}
	if promoted.Name != "standard" {
		t.Errorf("expected standard, got %s", promoted.Name)
	}
}

func TestApplyEffectivenessSafetyNet_PremiumNeverPromoted(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "premium", Models: []string{"claude-opus-4-6"}, RoutingMethod: "cold_start"}
	effectiveness := map[string]EffectivenessStats{
		"premium": {CorrectionRate: 0.95, AvgEffectiveness: 0.20, SessionCount: 30},
	}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("premium should never be promoted further")
	}
	if promoted.Name != "premium" {
		t.Errorf("expected premium, got %s", promoted.Name)
	}
}

func TestApplyEffectivenessSafetyNet_NoEffectivenessData(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "economy", Models: []string{"claude-haiku-4-5-20251001"}, RoutingMethod: "cold_start"}
	effectiveness := map[string]EffectivenessStats{}
	promoted, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("should not promote when no effectiveness data available")
	}
	if promoted.Name != "economy" {
		t.Errorf("expected economy, got %s", promoted.Name)
	}
	if result.Reason != "no effectiveness data" {
		t.Errorf("expected reason 'no effectiveness data', got %s", result.Reason)
	}
}

func TestApplyEffectivenessSafetyNet_BoundaryEconomy(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "economy", RoutingMethod: "cold_start"}
	// Exactly at threshold (0.6) should NOT trigger promotion
	effectiveness := map[string]EffectivenessStats{
		"economy": {CorrectionRate: 0.6, AvgEffectiveness: 0.70, SessionCount: 40},
	}
	_, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("exactly at threshold should not promote (must be > 0.6)")
	}
}

func TestApplyEffectivenessSafetyNet_BoundaryStandard(t *testing.T) {
	cfg := testConfig()
	tier := ModelTier{Name: "standard", RoutingMethod: "cold_start"}
	// Exactly at threshold (0.8) should NOT trigger promotion
	effectiveness := map[string]EffectivenessStats{
		"standard": {CorrectionRate: 0.8, AvgEffectiveness: 0.70, SessionCount: 100},
	}
	_, result := ApplyEffectivenessSafetyNet(tier, effectiveness, cfg.Tiers)
	if result.Promoted {
		t.Fatal("exactly at threshold should not promote (must be > 0.8)")
	}
}
