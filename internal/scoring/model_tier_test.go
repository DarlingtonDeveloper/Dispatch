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
