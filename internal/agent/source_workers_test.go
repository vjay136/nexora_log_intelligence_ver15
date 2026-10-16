package agent

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"
)

func TestSourceWorkerLimitDefaultsToBoundedAutoAndHonorsOne(t *testing.T) {
	config := Config{Inputs: make([]InputConfig, 3)}
	want := runtime.NumCPU()
	if want > 8 {
		want = 8
	}
	if want > len(config.Inputs) {
		want = len(config.Inputs)
	}
	if got := sourceWorkerLimit(config); got != want {
		t.Fatalf("auto workers = %d, want %d", got, want)
	}
	config.SourceWorkers = 1
	if got := sourceWorkerLimit(config); got != 1 {
		t.Fatalf("explicit workers = %d, want 1", got)
	}
}

func TestSourceWorkersRunTwoSourcesConcurrentlyAndPreserveSourceOrder(t *testing.T) {
	tails := []*tail{{input: InputConfig{Path: "source-a"}}, {input: InputConfig{Path: "source-b"}}}
	started := make(chan string, 2)
	continueWork := make(chan struct{})
	done := make(chan error, 1)
	var mu sync.Mutex
	order := map[string][]int{}
	go func() {
		done <- runSourceWorkers(context.Background(), tails, 2, func(source *tail) error {
			started <- source.input.Path
			<-continueWork
			for i := 0; i < 3; i++ {
				mu.Lock()
				order[source.input.Path] = append(order[source.input.Path], i)
				mu.Unlock()
			}
			return nil
		})
	}()
	seen := map[string]bool{}
	timeout := time.After(2 * time.Second)
	for range tails {
		select {
		case source := <-started:
			seen[source] = true
		case <-timeout:
			t.Fatal("source workers did not make progress concurrently")
		}
	}
	close(continueWork)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	if len(seen) != 2 {
		t.Fatalf("started sources = %#v", seen)
	}
	for _, source := range tails {
		got := order[source.input.Path]
		if len(got) != 3 || got[0] != 0 || got[1] != 1 || got[2] != 2 {
			t.Fatalf("source %s order = %v", source.input.Path, got)
		}
	}
}
