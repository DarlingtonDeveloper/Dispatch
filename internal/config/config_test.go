package config

import (
	"math"
	"os"
	"testing"
	"time"
)

func TestLoadDefaults(t *testing.T) {
	// Unset all DISPATCH_ env vars to test pure defaults
	envVars := []string{
		"DISPATCH_PORT", "DISPATCH_METRICS_PORT", "DISPATCH_ADMIN_TOKEN",
		"DISPATCH_DATABASE_URL", "DISPATCH_HERMES_URL", "DISPATCH_WARREN_URL",
		"DISPATCH_WARREN_TOKEN", "DISPATCH_FORGE_URL", "DISPATCH_ALEXANDRIA_URL",
		"DISPATCH_TICK_INTERVAL_MS", "DISPATCH_OWNER_FILTER_ENABLED", "DISPATCH_LOG_LEVEL",
	}
	for _, k := range envVars {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Port != 8600 {
		t.Errorf("expected port 8600, got %d", cfg.Server.Port)
	}
	if cfg.Server.MetricsPort != 8601 {
		t.Errorf("expected metrics port 8601, got %d", cfg.Server.MetricsPort)
	}
	if cfg.Hermes.URL != "nats://localhost:4222" {
		t.Errorf("expected nats URL, got %s", cfg.Hermes.URL)
	}
	if cfg.Warren.URL != "http://localhost:9090" {
		t.Errorf("expected warren URL, got %s", cfg.Warren.URL)
	}
	if cfg.PromptForge.URL != "http://localhost:8083" {
		t.Errorf("expected forge URL, got %s", cfg.PromptForge.URL)
	}
	if cfg.Alexandria.URL != "http://localhost:8500" {
		t.Errorf("expected alexandria URL, got %s", cfg.Alexandria.URL)
	}
	if cfg.Assignment.TickIntervalMs != 5000 {
		t.Errorf("expected tick 5000, got %d", cfg.Assignment.TickIntervalMs)
	}
	if cfg.Assignment.MaxConcurrentPerAgent != 3 {
		t.Errorf("expected max concurrent 3, got %d", cfg.Assignment.MaxConcurrentPerAgent)
	}
	if !cfg.Assignment.OwnerFilterEnabled {
		t.Error("expected owner filter enabled by default")
	}
	if cfg.Logging.Level != "info" {
		t.Errorf("expected log level 'info', got '%s'", cfg.Logging.Level)
	}
	if cfg.Logging.Format != "json" {
		t.Errorf("expected log format 'json', got '%s'", cfg.Logging.Format)
	}

	// Scoring defaults
	sw := cfg.Scoring.Weights
	expectedWeights := map[string]float64{
		"capability": 0.20, "availability": 0.10, "risk_fit": 0.12,
		"cost_efficiency": 0.10, "verifiability": 0.08, "reversibility": 0.08,
		"complexity_fit": 0.10, "uncertainty_fit": 0.07, "duration_fit": 0.05,
		"contextuality": 0.05, "subjectivity": 0.05,
	}
	actualWeights := map[string]float64{
		"capability": sw.Capability, "availability": sw.Availability, "risk_fit": sw.RiskFit,
		"cost_efficiency": sw.CostEfficiency, "verifiability": sw.Verifiability, "reversibility": sw.Reversibility,
		"complexity_fit": sw.ComplexityFit, "uncertainty_fit": sw.UncertaintyFit, "duration_fit": sw.DurationFit,
		"contextuality": sw.Contextuality, "subjectivity": sw.Subjectivity,
	}
	var weightSum float64
	for name, expected := range expectedWeights {
		actual := actualWeights[name]
		if math.Abs(actual-expected) > 0.001 {
			t.Errorf("scoring weight %s: expected %f, got %f", name, expected, actual)
		}
		weightSum += actual
	}
	if math.Abs(weightSum-1.0) > 0.001 {
		t.Errorf("scoring weights sum to %f, expected 1.0", weightSum)
	}
	if !cfg.Scoring.FastPathEnabled {
		t.Error("expected fast_path_enabled=true by default")
	}
	if cfg.Scoring.ParetoEnabled {
		t.Error("expected pareto_enabled=false by default")
	}

	// Duration helpers
	if cfg.TickInterval() != 5*time.Second {
		t.Errorf("expected TickInterval 5s, got %v", cfg.TickInterval())
	}
	if cfg.WakeTimeout() != 30*time.Second {
		t.Errorf("expected WakeTimeout 30s, got %v", cfg.WakeTimeout())
	}
	if cfg.DefaultTimeout() != 5*time.Minute {
		t.Errorf("expected DefaultTimeout 5m, got %v", cfg.DefaultTimeout())
	}
}

func TestLoadFromEnv(t *testing.T) {
	t.Setenv("DISPATCH_PORT", "9000")
	t.Setenv("DISPATCH_METRICS_PORT", "9001")
	t.Setenv("DISPATCH_ADMIN_TOKEN", "secret-token")
	t.Setenv("DISPATCH_DATABASE_URL", "postgres://localhost/dispatch_test")
	t.Setenv("DISPATCH_HERMES_URL", "nats://nats:4222")
	t.Setenv("DISPATCH_WARREN_URL", "http://warren:9090")
	t.Setenv("DISPATCH_WARREN_TOKEN", "warren-secret")
	t.Setenv("DISPATCH_FORGE_URL", "http://forge:8083")
	t.Setenv("DISPATCH_ALEXANDRIA_URL", "http://alex:8500")
	t.Setenv("DISPATCH_TICK_INTERVAL_MS", "2000")
	t.Setenv("DISPATCH_OWNER_FILTER_ENABLED", "false")
	t.Setenv("DISPATCH_LOG_LEVEL", "debug")

	cfg, err := Load("")
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Server.Port != 9000 {
		t.Errorf("expected port 9000, got %d", cfg.Server.Port)
	}
	if cfg.Server.MetricsPort != 9001 {
		t.Errorf("expected metrics port 9001, got %d", cfg.Server.MetricsPort)
	}
	if cfg.Server.AdminToken != "secret-token" {
		t.Errorf("expected admin token 'secret-token', got '%s'", cfg.Server.AdminToken)
	}
	if cfg.Database.URL != "postgres://localhost/dispatch_test" {
		t.Errorf("expected database URL, got '%s'", cfg.Database.URL)
	}
	if cfg.Hermes.URL != "nats://nats:4222" {
		t.Errorf("expected hermes URL, got '%s'", cfg.Hermes.URL)
	}
	if cfg.Warren.URL != "http://warren:9090" {
		t.Errorf("expected warren URL, got '%s'", cfg.Warren.URL)
	}
	if cfg.Warren.Token != "warren-secret" {
		t.Errorf("expected warren token, got '%s'", cfg.Warren.Token)
	}
	if cfg.PromptForge.URL != "http://forge:8083" {
		t.Errorf("expected forge URL, got '%s'", cfg.PromptForge.URL)
	}
	if cfg.Alexandria.URL != "http://alex:8500" {
		t.Errorf("expected alexandria URL, got '%s'", cfg.Alexandria.URL)
	}
	if cfg.Assignment.TickIntervalMs != 2000 {
		t.Errorf("expected tick 2000, got %d", cfg.Assignment.TickIntervalMs)
	}
	if cfg.Assignment.OwnerFilterEnabled {
		t.Error("expected owner filter disabled")
	}
	if cfg.Logging.Level != "debug" {
		t.Errorf("expected log level 'debug', got '%s'", cfg.Logging.Level)
	}
}
