// Package confidence computes parse confidence scores and routing decisions.
package confidence

// Route constants.
const (
	RouteNormal     = "normal"
	RouteWarning    = "warning"
	RouteQuarantine = "quarantine"
	RouteUnknown    = "unknown"
)

// Score computes a confidence score given evidence from the pipeline.
func Score(parserMatched bool, validationErrors int, unknownFields int, totalFields int, truncated bool) float64 {
	if !parserMatched {
		return 0.0
	}
	score := 1.0
	if validationErrors > 0 {
		score -= 0.05 * float64(validationErrors)
	}
	if totalFields > 0 {
		unknownRatio := float64(unknownFields) / float64(totalFields+unknownFields)
		score -= 0.3 * unknownRatio
	}
	if truncated {
		score -= 0.2
	}
	if score < 0 {
		score = 0
	}
	return score
}

// Route returns the routing decision for a given confidence score.
func Route(score float64) string {
	switch {
	case score >= 0.95:
		return RouteNormal
	case score >= 0.80:
		return RouteWarning
	case score >= 0.50:
		return RouteQuarantine
	default:
		return RouteUnknown
	}
}
