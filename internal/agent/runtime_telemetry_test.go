package agent

import (
	"bytes"
	"io"
	"log/slog"
	"strings"
	"testing"
	"time"
)

func TestStreamTelemetryFirstPublishStartsRateBaselineAtCurrentCounters(t *testing.T) {
	state := &streamState{nodeID: "node", sourceWorkers: 2}
	state.readLines.Store(40)
	state.bytes.Store(4000)
	first := state.publish("/source.log", "reading", 0, time.Unix(100, 0))
	if first.ReadLines != 40 || first.Bytes != 4000 {
		t.Fatalf("first sample lost cumulative counts: %+v", first)
	}
	state.readLines.Add(10)
	state.bytes.Add(1000)
	second := state.publish("/source.log", "reading", 0, time.Unix(101, 0))
	if second.ReadLinesPerSecond != 10 || second.BytesPerSecond != 1000 {
		t.Fatalf("rates include pre-baseline counters: %+v", second)
	}
}

func TestJournalProgressIsAggregatedAndOutputRecoveryIsTransitionOnly(t *testing.T) {
	var output bytes.Buffer
	previousLogger := slog.Default()
	slog.SetDefault(slog.New(slog.NewJSONHandler(&output, nil)))
	defer slog.SetDefault(previousLogger)
	journal := newJournal(time.Hour)
	journal.progress("/source.log", journalCounts{read: 100, parsed: 90, quarantined: 10})
	journal.progress("/source.log", journalCounts{read: 101, parsed: 91, quarantined: 10})
	journal.outputFailure("/source.log", "disk unavailable")
	journal.outputRecovered("/source.log")
	journal.outputRecovered("/source.log")
	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 5 { // progress + parsed + quarantined + failure + recovery
		t.Fatalf("journal emitted %d events, want one aggregate and one failure/recovery transition: %s", len(lines), output.String())
	}
	_ = io.Discard
}
