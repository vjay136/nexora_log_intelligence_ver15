package agent

import (
	"log/slog"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"nexora.local/nli-v15/telemetry"
)

type journalLimiter struct {
	mu       sync.Mutex
	last     map[string]time.Time
	previous map[string]journalCounts
	failed   map[string]bool
	interval time.Duration
}

type journalCounts struct{ read, parsed, quarantined uint64 }

type sampleSet struct {
	mu    sync.RWMutex
	files map[string]telemetry.StreamFileTelemetry
}

func newSampleSet() *sampleSet { return &sampleSet{files: map[string]telemetry.StreamFileTelemetry{}} }
func (s *sampleSet) put(sample telemetry.StreamFileTelemetry) {
	if sample.Source == "" {
		return
	}
	s.mu.Lock()
	if len(s.files) >= 256 {
		if _, exists := s.files[sample.Source]; !exists {
			s.mu.Unlock()
			return
		}
	}
	s.files[sample.Source] = sample
	s.mu.Unlock()
}
func (s *sampleSet) snapshot() []telemetry.StreamFileTelemetry {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]telemetry.StreamFileTelemetry, 0, len(s.files))
	for _, sample := range s.files {
		out = append(out, sample)
	}
	return out
}

func newJournal(interval time.Duration) *journalLimiter {
	return &journalLimiter{last: map[string]time.Time{}, previous: map[string]journalCounts{}, failed: map[string]bool{}, interval: interval}
}
func (j *journalLimiter) event(kind, source, detail string) {
	key := kind + "\x00" + source
	now := time.Now()
	j.mu.Lock()
	previous := j.last[key]
	if !previous.IsZero() && now.Sub(previous) < j.interval {
		j.mu.Unlock()
		return
	}
	j.last[key] = now
	j.mu.Unlock()
	attrs := []any{"source", source, "event", kind}
	if detail != "" {
		attrs = append(attrs, "detail", detail)
	}
	slog.Info("runtime", attrs...)
}

// progress emits one bounded aggregate per source and interval, keeping journal
// work proportional to batches rather than input records.
func (j *journalLimiter) progress(source string, current journalCounts) {
	now := time.Now()
	j.mu.Lock()
	key := "progress\x00" + source
	previous := j.previous[source]
	if last := j.last[key]; !last.IsZero() && now.Sub(last) < j.interval {
		j.mu.Unlock()
		return
	}
	j.last[key] = now
	j.previous[source] = current
	j.mu.Unlock()
	attrs := []any{"source", source, "event", "progress", "read_lines", current.read - previous.read, "parsed", current.parsed - previous.parsed, "quarantined", current.quarantined - previous.quarantined}
	slog.Info("runtime", attrs...)
	if delta := current.parsed - previous.parsed; delta > 0 {
		slog.Info("runtime", "source", source, "event", "parsed", "count", delta)
	}
	if delta := current.quarantined - previous.quarantined; delta > 0 {
		slog.Info("runtime", "source", source, "event", "quarantined", "count", delta)
	}
}

func (j *journalLimiter) outputFailure(source, detail string) {
	j.mu.Lock()
	j.failed[source] = true
	j.mu.Unlock()
	j.event("output_failure", source, detail)
}

func (j *journalLimiter) outputRecovered(source string) {
	j.mu.Lock()
	failed := j.failed[source]
	delete(j.failed, source)
	j.mu.Unlock()
	if failed {
		j.event("output_recovery", source, "output writes resumed")
	}
}

type streamState struct {
	nodeID                                                     string
	sourceWorkers                                              int
	thresholds                                                 telemetry.StreamThresholds
	capacity                                                   int
	readLines, normalized, quarantined, bytes                  atomic.Uint64
	readDuration, parseDuration, outputDuration, totalDuration atomic.Int64
	lastMu                                                     sync.Mutex
	last                                                       time.Time
	prevLines, prevNormalized, prevQuarantined, prevBytes      uint64
}

func (s *streamState) addParse(d time.Duration)  { s.parseDuration.Add(int64(d)) }
func (s *streamState) addOutput(d time.Duration) { s.outputDuration.Add(int64(d)) }
func (s *streamState) addTotal(d time.Duration)  { s.totalDuration.Add(int64(d)) }
func (s *streamState) publish(source, phase string, capacity int, now time.Time) telemetry.StreamFileTelemetry {
	s.lastMu.Lock()
	defer s.lastMu.Unlock()
	if s.last.IsZero() {
		s.prevLines = s.readLines.Load()
		s.prevNormalized = s.normalized.Load()
		s.prevQuarantined = s.quarantined.Load()
		s.prevBytes = s.bytes.Load()
		s.last = now
		return telemetry.StreamFileTelemetry{NodeID: s.nodeID, Source: source, Phase: phase, SourceWorkers: s.sourceWorkers, ReadLines: s.prevLines, Normalized: s.prevNormalized, Quarantined: s.prevQuarantined, Bytes: s.prevBytes, QueueCapacity: capacity, Thresholds: s.thresholds, UpdatedAt: now.UTC()}
	}
	elapsed := now.Sub(s.last).Seconds()
	if elapsed <= 0 {
		return telemetry.StreamFileTelemetry{}
	}
	rl := s.readLines.Load()
	no := s.normalized.Load()
	q := s.quarantined.Load()
	b := s.bytes.Load()
	// The runtime currently has no asynchronous record queue. Report zero queue
	// depth/capacity instead of presenting processing throughput as backlog.
	depth := 0
	sample := telemetry.StreamFileTelemetry{NodeID: s.nodeID, Source: source, Phase: phase, SourceWorkers: s.sourceWorkers, ReadLinesPerSecond: float64(rl-s.prevLines) / elapsed, NormalizedPerSecond: float64(no-s.prevNormalized) / elapsed, QuarantinedPerSecond: float64(q-s.prevQuarantined) / elapsed, BytesPerSecond: float64(b-s.prevBytes) / elapsed, QueueDepth: depth, QueueCapacity: 0, Backpressure: false, ReadDuration: time.Duration(s.readDuration.Load()), ParseDuration: time.Duration(s.parseDuration.Load()), OutputDuration: time.Duration(s.outputDuration.Load()), TotalDuration: time.Duration(s.totalDuration.Load()), ReadLines: rl, Normalized: no, Quarantined: q, Bytes: b, UpdatedAt: now.UTC(), Thresholds: s.thresholds}
	s.prevLines, s.prevNormalized, s.prevQuarantined, s.prevBytes, s.last = rl, no, q, b, now
	return sample
}

func init() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelInfo})))
}
