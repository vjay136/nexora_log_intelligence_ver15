package progressive

import (
	"sort"
	"time"
)

// RolloutMetrics is computed only from reported node outcomes.
type RolloutMetrics struct {
	TotalNodes      int           `json:"total_nodes"`
	PendingNodes    int           `json:"pending_nodes"`
	RunningNodes    int           `json:"running_nodes"`
	SucceededNodes  int           `json:"succeeded_nodes"`
	FailedNodes     int           `json:"failed_nodes"`
	RolledBackNodes int           `json:"rolled_back_nodes"`
	SuccessRate     float64       `json:"success_rate"`
	ErrorRate       float64       `json:"error_rate"`
	LatencyP50      time.Duration `json:"latency_p50"`
	LatencyP95      time.Duration `json:"latency_p95"`
}

func calculateMetrics(results map[string]*NodeResult) RolloutMetrics {
	metrics := RolloutMetrics{TotalNodes: len(results)}
	latencies := make([]time.Duration, 0, len(results))
	for _, result := range results {
		switch result.Status {
		case NodePending:
			metrics.PendingNodes++
		case NodeRunning:
			metrics.RunningNodes++
		case NodeSucceeded:
			metrics.SucceededNodes++
		case NodeFailed:
			metrics.FailedNodes++
		case NodeRolledBack:
			metrics.RolledBackNodes++
		}
		if result.Latency > 0 {
			latencies = append(latencies, result.Latency)
		}
	}
	terminal := metrics.SucceededNodes + metrics.FailedNodes
	if terminal > 0 {
		metrics.SuccessRate = 100 * float64(metrics.SucceededNodes) / float64(terminal)
		metrics.ErrorRate = 100 * float64(metrics.FailedNodes) / float64(terminal)
	}
	sort.Slice(latencies, func(i, j int) bool { return latencies[i] < latencies[j] })
	metrics.LatencyP50 = percentileDuration(latencies, 50)
	metrics.LatencyP95 = percentileDuration(latencies, 95)
	return metrics
}

func percentileDuration(values []time.Duration, percentile int) time.Duration {
	if len(values) == 0 {
		return 0
	}
	index := (len(values)*percentile + 99) / 100
	if index < 1 {
		index = 1
	}
	if index > len(values) {
		index = len(values)
	}
	return values[index-1]
}
