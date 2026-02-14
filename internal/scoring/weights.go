package scoring

import (
	"fmt"
	"math"
)

// WeightSet defines the relative importance of each scoring factor.
// All weights must sum to 1.0 (±0.001 tolerance).
type WeightSet struct {
	Capability     float64
	Availability   float64
	RiskFit        float64
	CostEfficiency float64
	Verifiability  float64
	Reversibility  float64
	ComplexityFit  float64
	UncertaintyFit float64
	DurationFit    float64
	Contextuality  float64
	Subjectivity   float64
}

// DefaultWeights returns the spec-defined weight distribution.
func DefaultWeights() WeightSet {
	return WeightSet{
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
	}
}

// Sum returns the total of all weights.
func (w WeightSet) Sum() float64 {
	return w.Capability + w.Availability + w.RiskFit + w.CostEfficiency +
		w.Verifiability + w.Reversibility + w.ComplexityFit +
		w.UncertaintyFit + w.DurationFit + w.Contextuality + w.Subjectivity
}

// Validate checks that weights sum to 1.0 and none are negative.
func (w WeightSet) Validate() error {
	if math.Abs(w.Sum()-1.0) > 0.001 {
		return fmt.Errorf("weights sum to %.4f, must sum to 1.0", w.Sum())
	}
	for _, v := range w.asList() {
		if v < 0 {
			return fmt.Errorf("negative weight: %f", v)
		}
	}
	return nil
}

func (w WeightSet) asList() []float64 {
	return []float64{
		w.Capability, w.Availability, w.RiskFit, w.CostEfficiency,
		w.Verifiability, w.Reversibility, w.ComplexityFit,
		w.UncertaintyFit, w.DurationFit, w.Contextuality, w.Subjectivity,
	}
}
