package scoring

// ParetoCandidate represents a candidate scored across multiple dimensions.
type ParetoCandidate struct {
	AgentSlug string  `json:"agent_slug"`
	Speed     float64 `json:"speed"`
	Cost      float64 `json:"cost"`
	Quality   float64 `json:"quality"`
	Risk      float64 `json:"risk"` // lower is better
}

// ComputeFrontier returns the Pareto-optimal candidates from the input set.
// A candidate is dominated if another candidate is >= on all dimensions
// (with <= on risk, since lower risk is better) and strictly better on at least one.
// O(n^2) dominance check — fine for typical candidate set sizes.
func ComputeFrontier(candidates []ParetoCandidate) []ParetoCandidate {
	if len(candidates) <= 1 {
		return candidates
	}

	var frontier []ParetoCandidate
	for i := range candidates {
		dominated := false
		for j := range candidates {
			if i == j {
				continue
			}
			if dominates(candidates[j], candidates[i]) {
				dominated = true
				break
			}
		}
		if !dominated {
			frontier = append(frontier, candidates[i])
		}
	}
	return frontier
}

// dominates returns true if a dominates b.
// For speed, cost, quality: higher is better.
// For risk: lower is better.
func dominates(a, b ParetoCandidate) bool {
	// a must be >= b on all "higher is better" dimensions and <= on risk
	if a.Speed < b.Speed || a.Cost < b.Cost || a.Quality < b.Quality || a.Risk > b.Risk {
		return false
	}
	// a must be strictly better on at least one dimension
	return a.Speed > b.Speed || a.Cost > b.Cost || a.Quality > b.Quality || a.Risk < b.Risk
}
