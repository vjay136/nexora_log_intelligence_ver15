package ai

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestPendingQueueIsBoundedAndObservable(t *testing.T) {
	d := New(Config{Enabled: true, MaxPendingClusters: 2}, nil, nil, t.TempDir())
	d.Submit(1, "one")
	d.Submit(2, "two")
	d.Submit(3, "three")

	stats := d.Stats()
	if stats.PendingClusters != 2 || stats.DroppedPending != 1 {
		t.Fatalf("unexpected queue stats: %+v", stats)
	}
}

func TestSamplesAreUTF8SafeAndBounded(t *testing.T) {
	d := New(Config{Enabled: true, MaxSampleBytes: 7}, nil, nil, t.TempDir())
	d.Submit(1, "abc€defgh")

	if len(d.pending) != 1 || len(d.pending[0].Samples) != 1 {
		t.Fatalf("sample was not queued: %#v", d.pending)
	}
	got := d.pending[0].Samples[0]
	if len(got) > 7 || !utf8.ValidString(got) {
		t.Fatalf("sample is not safely bounded UTF-8: %q (%d bytes)", got, len(got))
	}
	if d.Stats().TruncatedSamples != 1 {
		t.Fatalf("expected a visible truncation count, got %+v", d.Stats())
	}

	d.Submit(1, strings.Repeat("x", 100))
	if len(d.pending[0].Samples[1]) > 7 {
		t.Fatalf("additional sample exceeded configured bound")
	}
}

func TestMaxSampleBytesUsesHardCeiling(t *testing.T) {
	d := New(Config{Enabled: true, MaxSampleBytes: hardMaxSampleBytes * 2}, nil, nil, t.TempDir())
	d.Submit(1, strings.Repeat("x", hardMaxSampleBytes+1))
	if got := len(d.pending[0].Samples[0]); got != hardMaxSampleBytes {
		t.Fatalf("sample length = %d, want hard ceiling %d", got, hardMaxSampleBytes)
	}
}
