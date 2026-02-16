package scoring

import (
	"path"

	"github.com/MikeSquared-Agency/Dispatch/internal/config"
	"github.com/MikeSquared-Agency/Dispatch/internal/store"
)

// ModelTier represents a resolved model tier with its available models.
type ModelTier struct {
	Name          string
	Models        []string
	RoutingMethod string // "cold_start" or "learned"
}

// DeriveModelTier selects the appropriate model tier for a task.
// When hasLearnedData is false (cold start), static rules are used.
// When true, the scoring engine route derives tier from complexity/risk/reversibility.
func DeriveModelTier(task *store.Task, cfg config.ModelRoutingConfig, hasLearnedData bool) ModelTier {
	if !cfg.Enabled {
		tier := tierByName(cfg.DefaultTier, cfg.Tiers)
		tier.RoutingMethod = "cold_start"
		return tier
	}

	routingMethod := "cold_start"
	if hasLearnedData {
		routingMethod = "learned"
	}

	// One-way-door override: always premium
	if task.OneWayDoor {
		tier := tierByName("premium", cfg.Tiers)
		tier.RoutingMethod = routingMethod
		return tier
	}

	// High risk override: risk >= 0.8 → premium
	if task.RiskScore != nil && *task.RiskScore >= 0.8 {
		tier := tierByName("premium", cfg.Tiers)
		tier.RoutingMethod = routingMethod
		return tier
	}

	if hasLearnedData {
		tier := scoringEngineRoute(task, cfg)
		tier.RoutingMethod = "learned"
		return tier
	}

	// Cold start path
	if match := ColdStartRoute(task, cfg.ColdStartRules); match != nil {
		tier := tierByName(match.Name, cfg.Tiers)
		tier.RoutingMethod = "cold_start"
		return tier
	}

	tier := tierByName(cfg.DefaultTier, cfg.Tiers)
	tier.RoutingMethod = "cold_start"
	return tier
}

// ColdStartRoute applies static rules to determine a model tier without historical data.
// Returns nil if no rule matches.
func ColdStartRoute(task *store.Task, rules []config.ColdStartRule) *ModelTier {
	for _, rule := range rules {
		if matchesColdStartRule(task, rule) {
			tier := ModelTier{Name: rule.Tier}
			return &tier
		}
	}
	return nil
}

// scoringEngineRoute derives model tier from task scoring dimensions.
func scoringEngineRoute(task *store.Task, cfg config.ModelRoutingConfig) ModelTier {
	complexity := 0.5
	if task.ComplexityScore != nil {
		complexity = *task.ComplexityScore
	}
	risk := 0.5
	if task.RiskScore != nil {
		risk = *task.RiskScore
	}
	reversibility := 0.5
	if task.ReversibilityScore != nil {
		reversibility = *task.ReversibilityScore
	}

	// Composite score: higher = needs more capable model
	// High complexity, high risk, low reversibility → premium
	score := complexity*0.4 + risk*0.35 + (1.0-reversibility)*0.25

	switch {
	case score < 0.3:
		return tierByName("economy", cfg.Tiers)
	case score < 0.6:
		return tierByName("standard", cfg.Tiers)
	default:
		return tierByName("premium", cfg.Tiers)
	}
}

// matchesColdStartRule checks whether a task matches a cold start rule.
// A rule matches if ALL specified criteria are satisfied:
//   - Labels: task must have at least one label from the rule's label set
//   - FilePatterns: all task file patterns must match at least one rule glob
//   - MaxFiles: task must have <= MaxFiles file patterns (0 means unchecked)
func matchesColdStartRule(task *store.Task, rule config.ColdStartRule) bool {
	// Labels check: task must have at least one matching label
	if len(rule.Labels) > 0 {
		if !hasAnyLabel(task.Labels, rule.Labels) {
			return false
		}
	}

	// File pattern check: every task file pattern must match at least one rule glob
	if len(rule.FilePatterns) > 0 && len(task.FilePatterns) > 0 {
		for _, taskFP := range task.FilePatterns {
			if !matchesAnyGlob(taskFP, rule.FilePatterns) {
				return false
			}
		}
	}

	// MaxFiles check
	if rule.MaxFiles > 0 && len(task.FilePatterns) > rule.MaxFiles {
		return false
	}

	return true
}

// RuntimeForTier returns the appropriate runtime for a given tier and file count.
func RuntimeForTier(tierName string, fileCount int) string {
	switch tierName {
	case "economy":
		return "picoclaw"
	case "standard":
		if fileCount <= 1 {
			return "picoclaw"
		}
		return "openclaw"
	case "premium":
		return "openclaw"
	default:
		return "openclaw"
	}
}

func tierByName(name string, tiers []config.ModelTierDef) ModelTier {
	for _, t := range tiers {
		if t.Name == name {
			return ModelTier{Name: t.Name, Models: t.Models}
		}
	}
	return ModelTier{Name: name}
}

func hasAnyLabel(taskLabels, ruleLabels []string) bool {
	for _, tl := range taskLabels {
		for _, rl := range ruleLabels {
			if tl == rl {
				return true
			}
		}
	}
	return false
}

func matchesAnyGlob(filename string, patterns []string) bool {
	for _, p := range patterns {
		if matched, _ := path.Match(p, filename); matched {
			return true
		}
	}
	return false
}
