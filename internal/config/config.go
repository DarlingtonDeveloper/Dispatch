package config

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Server      ServerConfig     `yaml:"server"`
	Database    DatabaseConfig   `yaml:"database"`
	Hermes      HermesConfig     `yaml:"hermes"`
	Warren      WarrenConfig     `yaml:"warren"`
	PromptForge ForgeConfig      `yaml:"promptforge"`
	Alexandria  AlexandriaConfig `yaml:"alexandria"`
	Assignment  AssignmentConfig `yaml:"assignment"`
	Scoring     ScoringConfig    `yaml:"scoring"`
	Logging     LoggingConfig    `yaml:"logging"`
}

type ServerConfig struct {
	Port        int    `yaml:"port"`
	MetricsPort int    `yaml:"metrics_port"`
	AdminToken  string `yaml:"admin_token"`
}

type DatabaseConfig struct {
	URL string `yaml:"url"`
}

type HermesConfig struct {
	URL string `yaml:"url"`
}

type WarrenConfig struct {
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type ForgeConfig struct {
	URL string `yaml:"url"`
}

type AlexandriaConfig struct {
	URL string `yaml:"url"`
}

type AssignmentConfig struct {
	TickIntervalMs        int  `yaml:"tick_interval_ms"`
	WakeTimeoutMs         int  `yaml:"wake_timeout_ms"`
	DefaultTimeoutMs      int  `yaml:"default_timeout_ms"`
	MaxConcurrentPerAgent int  `yaml:"max_concurrent_per_agent"`
	OwnerFilterEnabled    bool `yaml:"owner_filter_enabled"`
}

type ScoringConfig struct {
	Weights         ScoringWeights `yaml:"weights"`
	FastPathEnabled bool           `yaml:"fast_path_enabled"`
	ParetoEnabled   bool           `yaml:"pareto_enabled"`
}

type ScoringWeights struct {
	Capability     float64 `yaml:"capability"`
	Availability   float64 `yaml:"availability"`
	RiskFit        float64 `yaml:"risk_fit"`
	CostEfficiency float64 `yaml:"cost_efficiency"`
	Verifiability  float64 `yaml:"verifiability"`
	Reversibility  float64 `yaml:"reversibility"`
	ComplexityFit  float64 `yaml:"complexity_fit"`
	UncertaintyFit float64 `yaml:"uncertainty_fit"`
	DurationFit    float64 `yaml:"duration_fit"`
	Contextuality  float64 `yaml:"contextuality"`
	Subjectivity   float64 `yaml:"subjectivity"`
}

type LoggingConfig struct {
	Level  string `yaml:"level"`
	Format string `yaml:"format"`
}

func (c *Config) TickInterval() time.Duration {
	return time.Duration(c.Assignment.TickIntervalMs) * time.Millisecond
}

func (c *Config) WakeTimeout() time.Duration {
	return time.Duration(c.Assignment.WakeTimeoutMs) * time.Millisecond
}

func (c *Config) DefaultTimeout() time.Duration {
	return time.Duration(c.Assignment.DefaultTimeoutMs) * time.Millisecond
}

func Load(path string) (*Config, error) {
	cfg := &Config{
		Server: ServerConfig{
			Port:        8600,
			MetricsPort: 8601,
		},
		Hermes: HermesConfig{
			URL: "nats://localhost:4222",
		},
		Warren: WarrenConfig{
			URL: "http://localhost:9090",
		},
		PromptForge: ForgeConfig{
			URL: "http://localhost:8083",
		},
		Alexandria: AlexandriaConfig{
			URL: "http://localhost:8500",
		},
		Assignment: AssignmentConfig{
			TickIntervalMs:        5000,
			WakeTimeoutMs:         30000,
			DefaultTimeoutMs:      300000,
			MaxConcurrentPerAgent: 3,
			OwnerFilterEnabled:    true,
		},
		Scoring: ScoringConfig{
			Weights: ScoringWeights{
				Capability:     0.20,
				Availability:   0.10,
				RiskFit:        0.12,
				CostEfficiency: 0.10,
				Verifiability:  0.08,
				Reversibility:  0.08,
				ComplexityFit:  0.10,
				UncertaintyFit: 0.07,
				DurationFit:    0.05,
				Contextuality:  0.05,
				Subjectivity:   0.05,
			},
			FastPathEnabled: true,
			ParetoEnabled:   false,
		},
		Logging: LoggingConfig{
			Level:  "info",
			Format: "json",
		},
	}

	if path != "" {
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, fmt.Errorf("read config: %w", err)
		}
		if err := yaml.Unmarshal(data, cfg); err != nil {
			return nil, fmt.Errorf("parse config: %w", err)
		}
	}

	applyEnv(cfg)
	return cfg, nil
}

func applyEnv(cfg *Config) {
	if v := os.Getenv("DISPATCH_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.Port = n
		}
	}
	if v := os.Getenv("DISPATCH_METRICS_PORT"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Server.MetricsPort = n
		}
	}
	if v := os.Getenv("DISPATCH_ADMIN_TOKEN"); v != "" {
		cfg.Server.AdminToken = v
	}
	if v := os.Getenv("DISPATCH_DATABASE_URL"); v != "" {
		cfg.Database.URL = v
	}
	if v := os.Getenv("DISPATCH_HERMES_URL"); v != "" {
		cfg.Hermes.URL = v
	}
	if v := os.Getenv("DISPATCH_WARREN_URL"); v != "" {
		cfg.Warren.URL = v
	}
	if v := os.Getenv("DISPATCH_WARREN_TOKEN"); v != "" {
		cfg.Warren.Token = v
	}
	if v := os.Getenv("DISPATCH_FORGE_URL"); v != "" {
		cfg.PromptForge.URL = v
	}
	if v := os.Getenv("DISPATCH_ALEXANDRIA_URL"); v != "" {
		cfg.Alexandria.URL = v
	}
	if v := os.Getenv("DISPATCH_TICK_INTERVAL_MS"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			cfg.Assignment.TickIntervalMs = n
		}
	}
	if v := os.Getenv("DISPATCH_OWNER_FILTER_ENABLED"); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			cfg.Assignment.OwnerFilterEnabled = b
		}
	}
	if v := os.Getenv("DISPATCH_LOG_LEVEL"); v != "" {
		cfg.Logging.Level = v
	}
}
