package controlplane

import (
	"math"
	"net/http"
	"runtime"
	"sort"
	"strings"
	"sync"
	"time"

	"nexora.local/nli-v15/internal/fleet"
	metastore "nexora.local/nli-v15/internal/store"
)

type aiUsageEvent struct {
	At        time.Time
	Success   bool
	LatencyMs float64
}

type aiUsageStore struct {
	mu     sync.RWMutex
	events []aiUsageEvent
}

func (s *aiUsageStore) record(success bool, latency time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	s.events = append(s.events, aiUsageEvent{At: time.Now().UTC(), Success: success, LatencyMs: float64(latency) / float64(time.Millisecond)})
	if len(s.events) > 4096 {
		s.events = append([]aiUsageEvent(nil), s.events[len(s.events)-4096:]...)
	}
	s.mu.Unlock()
}

func (s *aiUsageStore) window(after, before time.Time) []aiUsageEvent {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]aiUsageEvent, 0)
	for _, item := range s.events {
		if !item.At.Before(after) && !item.At.After(before) {
			out = append(out, item)
		}
	}
	return out
}

func (s *aiUsageStore) snapshot() (requests, successes, failures int, latencyAverage float64) {
	if s == nil {
		return 0, 0, 0, 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	var latency float64
	for _, item := range s.events {
		requests++
		if item.Success {
			successes++
		} else {
			failures++
		}
		latency += item.LatencyMs
	}
	if requests > 0 {
		latencyAverage = latency / float64(requests)
	}
	return
}

// handleAIUsage supplies the dashboard with bounded in-memory request metrics
// and the current review queue size. Provider tokens are deliberately not
// inferred when the upstream API does not return verified accounting.
func (s *Server) handleAIUsage(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	requests, successes, failures, latency := s.aiMetrics.snapshot()
	pending := 0
	if drafts, err := s.metadata.List(r.Context(), "ai-proposals", metastore.Query{State: "draft", Limit: 1000}); err == nil {
		pending = len(drafts)
	}
	jsonOK(w, map[string]any{
		"requests": requests, "successes": successes, "failures": failures,
		"latency_ms_avg": latency, "pending_drafts": pending,
		"tokens": 0, "token_accounting_available": false,
		"retention": "bounded_controller_memory",
	})
}

type metricBucket struct {
	time                  time.Time
	received, parsed      int
	quarantined, errors   int
	latencySum            float64
	latencyCount          int
	aiRequests, aiSuccess int
	aiFailure             int
	aiLatencySum          float64
	aiLatencyCount        int
}

func parseMetricDuration(value string, choices map[string]time.Duration, fallback time.Duration) (time.Duration, bool) {
	if value == "" {
		return fallback, true
	}
	duration, ok := choices[value]
	return duration, ok
}

func (s *Server) handleMetricsTimeSeries(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		w.Header().Set("Allow", "GET")
		jsonError(w, "GET required", http.StatusMethodNotAllowed)
		return
	}
	ranges := map[string]time.Duration{"1h": time.Hour, "6h": 6 * time.Hour, "24h": 24 * time.Hour, "7d": 7 * 24 * time.Hour, "30d": 30 * 24 * time.Hour}
	steps := map[string]time.Duration{"1m": time.Minute, "5m": 5 * time.Minute, "15m": 15 * time.Minute, "1h": time.Hour, "6h": 6 * time.Hour, "1d": 24 * time.Hour}
	rangeName := r.URL.Query().Get("range")
	stepName := r.URL.Query().Get("step")
	window, ok := parseMetricDuration(rangeName, ranges, time.Hour)
	if !ok {
		jsonError(w, "range must be one of 1h, 6h, 24h, 7d or 30d", http.StatusBadRequest)
		return
	}
	step, ok := parseMetricDuration(stepName, steps, time.Minute)
	if !ok || step > window || window%step != 0 || int(window/step) > 1000 {
		jsonError(w, "step is unsupported or would create more than 1000 buckets", http.StatusBadRequest)
		return
	}
	nodeID := strings.TrimSpace(r.URL.Query().Get("node_id"))
	if len(nodeID) > 128 {
		jsonError(w, "node_id exceeds 128 bytes", http.StatusBadRequest)
		return
	}
	// Include the current partial interval so a newly accepted client record is
	// visible immediately instead of waiting for the next bucket boundary.
	to := time.Now().UTC().Truncate(step).Add(step)
	from := to.Add(-window)
	bucketCount := int(window / step)
	buckets := make([]metricBucket, bucketCount)
	for index := range buckets {
		buckets[index].time = from.Add(time.Duration(index) * step)
	}
	indexFor := func(value time.Time) int {
		index := int(value.Sub(from) / step)
		if index < 0 || index >= len(buckets) {
			return -1
		}
		return index
	}
	for _, record := range s.events.Window(from, to, nodeID) {
		index := indexFor(record.Timestamp)
		if index < 0 {
			continue
		}
		bucket := &buckets[index]
		bucket.received++
		if record.Route == "normal" && record.Result != "quarantined" {
			bucket.parsed++
		}
		if record.Route == "quarantine" || record.Result == "quarantined" {
			bucket.quarantined++
		}
		if len(record.ErrorCodes) > 0 || record.Route == "error" {
			bucket.errors++
		}
		if record.LatencyMs > 0 {
			bucket.latencySum += float64(record.LatencyMs)
			bucket.latencyCount++
		}
	}
	for _, event := range s.aiMetrics.window(from, to) {
		index := indexFor(event.At)
		if index < 0 {
			continue
		}
		bucket := &buckets[index]
		bucket.aiRequests++
		if event.Success {
			bucket.aiSuccess++
		} else {
			bucket.aiFailure++
		}
		bucket.aiLatencySum += event.LatencyMs
		bucket.aiLatencyCount++
	}
	points := make([]map[string]any, 0, len(buckets))
	for _, bucket := range buckets {
		latency, aiLatency := 0.0, 0.0
		if bucket.latencyCount > 0 {
			latency = bucket.latencySum / float64(bucket.latencyCount)
		}
		if bucket.aiLatencyCount > 0 {
			aiLatency = bucket.aiLatencySum / float64(bucket.aiLatencyCount)
		}
		parseRate := 0.0
		if bucket.received > 0 {
			parseRate = float64(bucket.parsed) * 100 / float64(bucket.received)
		}
		points = append(points, map[string]any{"timestamp": bucket.time.Format(time.RFC3339), "logs_received": bucket.received, "logs_parsed": bucket.parsed, "logs_quarantined": bucket.quarantined, "errors": bucket.errors, "parse_latency_ms_avg": latency, "parse_rate_pct": parseRate, "ai_requests": bucket.aiRequests, "ai_success": bucket.aiSuccess, "ai_failure": bucket.aiFailure, "ai_latency_ms_avg": aiLatency})
	}
	fleetSnapshot := s.fleet.All()
	active := make([]*fleet.Node, 0, len(fleetSnapshot))
	for _, node := range fleetSnapshot {
		if nodeID == "" || node.ID == nodeID {
			if node.Metrics != nil {
				active = append(active, node)
			}
		}
	}
	sort.Slice(active, func(i, j int) bool { return active[i].ID < active[j].ID })
	clientMetrics := make([]map[string]any, 0, len(active))
	var cpuSum, memorySum, loadSum float64
	var cpuCount, memoryCount, loadCount int
	for _, node := range active {
		metric := node.Metrics
		clientMetrics = append(clientMetrics, map[string]any{"node_id": node.ID, "hostname": node.Hostname, "updated_at": metric.UpdatedAt, "cpu_percent": metric.CPUPercent, "memory_bytes": metric.MemoryBytes, "memory_percent": metric.MemoryPercent, "load_1m": metric.Load1, "load_5m": metric.Load5, "load_15m": metric.Load15, "parse_latency_ms": metric.ParseLatencyMs})
		if metric.CPUPercent > 0 {
			cpuSum += metric.CPUPercent
			cpuCount++
		}
		if metric.MemoryPercent > 0 {
			memorySum += metric.MemoryPercent
			memoryCount++
		}
		if metric.Load1 > 0 {
			loadSum += metric.Load1
			loadCount++
		}
	}
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	system := map[string]any{"controller_cpu_count": runtime.NumCPU(), "controller_memory_alloc_bytes": mem.Alloc, "controller_memory_sys_bytes": mem.Sys, "controller_goroutines": runtime.NumGoroutine(), "client_cpu_percent_avg": nullableAverage(cpuSum, cpuCount), "client_memory_percent_avg": nullableAverage(memorySum, memoryCount), "client_load_1m_avg": nullableAverage(loadSum, loadCount), "availability": map[string]bool{"controller_cpu_percent": false, "controller_memory": true, "client_cpu_percent": cpuCount > 0, "client_memory_percent": memoryCount > 0, "client_load": loadCount > 0}}
	jsonOK(w, map[string]any{"schema_version": "1", "range": rangeNameOrDefault(rangeName, "1h"), "step": rangeNameOrDefault(stepName, "1m"), "from": from.Format(time.RFC3339), "to": to.Format(time.RFC3339), "node_id": nodeID, "points": points, "fleet": map[string]any{"stats": s.fleet.Stats(), "clients": clientMetrics}, "system": system, "retention": map[string]any{"raw_logs": false, "event_payloads": false, "secrets": false, "series_source": "bounded_parser_metadata_and_latest_heartbeat"}})
}

func nullableAverage(total float64, count int) any {
	if count == 0 || math.IsNaN(total) || math.IsInf(total, 0) {
		return nil
	}
	return total / float64(count)
}

func rangeNameOrDefault(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}
