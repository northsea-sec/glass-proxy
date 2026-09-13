package debug


import (
	"fmt"
	"math"
)

// BackendProfile defines the expected timing characteristics of an inference backend.
type BackendProfile struct {
	Name         string
	Location     string
	ITTRange     [2]float64 // [min, max] in ms
	TPSRange     [2]float64 // [min, max] tokens/sec
	VarianceRange [2]float64 // [min, max] coefficient of variation
}

// Known backend profiles (ported from fingerprint_db.py KNOWN_BACKENDS).
var KnownBackends = map[string]BackendProfile{
	"trainium": {
		Name: "AWS Trainium", Location: "US-East",
		ITTRange: [2]float64{35, 70}, TPSRange: [2]float64{8, 15}, VarianceRange: [2]float64{0.3, 0.8},
	},
	"tpu": {
		Name: "Google TPU", Location: "GCP",
		ITTRange: [2]float64{25, 50}, TPSRange: [2]float64{12, 25}, VarianceRange: [2]float64{0.2, 0.6},
	},
	"gpu": {
		Name: "Standard GPU", Location: "Various",
		ITTRange: [2]float64{50, 100}, TPSRange: [2]float64{5, 12}, VarianceRange: [2]float64{0.4, 1.0},
	},
}

// ThinkingTier classifies thinking budget into display tiers.
func ThinkingTier(budget int) string {
	if budget >= 20000 {
		return "ultra"
	} else if budget >= 8000 {
		return "enhanced"
	} else if budget >= 1024 {
		return "basic"
	}
	return "none"
}

// ClassifyBackend determines the inference backend from ITT characteristics.
// Returns (backend_name, confidence_0_100, evidence_map).
// Ported from fingerprint_db.py classify_backend().
func ClassifyBackend(ittMean, tps, variance float64) (string, float64, map[string][]string) {
	if ittMean == 0 && tps == 0 {
		return "gpu", 0, map[string][]string{"gpu": {"No timing data"}}
	}

	// TPS-only fallback
	if ittMean == 0 && tps > 0 {
		evidence := map[string][]string{}
		if tps >= 12 {
			evidence["tpu"] = []string{fmt.Sprintf("TPS %.1f (no ITT)", tps)}
			return "tpu", math.Min(40, tps*2), evidence
		} else if tps >= 8 {
			evidence["trainium"] = []string{fmt.Sprintf("TPS %.1f (no ITT)", tps)}
			return "trainium", math.Min(35, tps*2), evidence
		}
		evidence["gpu"] = []string{fmt.Sprintf("TPS %.1f (no ITT)", tps)}
		return "gpu", math.Min(30, tps*3), evidence
	}

	scores := map[string]float64{}
	evidence := map[string][]string{}

	for id, profile := range KnownBackends {
		score := 0.0
		var ev []string

		// ITT scoring (weight: 0.5)
		ittMin, ittMax := profile.ITTRange[0], profile.ITTRange[1]
		if ittMin <= ittMean && ittMean <= ittMax {
			center := (ittMin + ittMax) / 2
			distance := math.Abs(ittMean-center) / ((ittMax - ittMin) / 2)
			score += (1 - distance) * 0.5
			ev = append(ev, fmt.Sprintf("ITT %.1fms in [%.0f-%.0f]", ittMean, ittMin, ittMax))
		} else if ittMean < ittMin {
			score += math.Max(0, 0.3-(ittMin-ittMean)/50)
		} else {
			score += math.Max(0, 0.3-(ittMean-ittMax)/50)
		}

		// TPS scoring (weight: 0.3)
		tpsMin, tpsMax := profile.TPSRange[0], profile.TPSRange[1]
		if tpsMin <= tps && tps <= tpsMax {
			center := (tpsMin + tpsMax) / 2
			distance := math.Abs(tps-center) / ((tpsMax - tpsMin) / 2)
			score += (1 - distance) * 0.3
			ev = append(ev, fmt.Sprintf("TPS %.1f in [%.0f-%.0f]", tps, tpsMin, tpsMax))
		}

		// Variance scoring (weight: 0.2)
		varMin, varMax := profile.VarianceRange[0], profile.VarianceRange[1]
		if varMin <= variance && variance <= varMax {
			score += 0.2
			ev = append(ev, fmt.Sprintf("Var %.2f in [%.1f-%.1f]", variance, varMin, varMax))
		}

		scores[id] = score
		if len(ev) > 0 {
			evidence[id] = ev
		}
	}

	// Find best match
	bestBackend := "gpu"
	bestScore := 0.0
	for id, score := range scores {
		if score > bestScore {
			bestScore = score
			bestBackend = id
		}
	}

	confidence := math.Min(100, bestScore*100)

	// Never return unknown — always classify
	if confidence == 0 {
		return "gpu", 0, map[string][]string{"gpu": {"Default fallback"}}
	}

	return bestBackend, confidence, evidence
}

// needed for fmt.Sprintf in this file