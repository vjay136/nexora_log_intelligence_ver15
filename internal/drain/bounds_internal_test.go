package drain

import (
	"testing"
	"time"
)

func TestMaxClustersHasFiniteDefaultAndHardCap(t *testing.T) {
	if got := New(Config{}).cfg.MaxClusters; got != defaultMaxClusters {
		t.Fatalf("default MaxClusters = %d, want %d", got, defaultMaxClusters)
	}
	if got := New(Config{MaxClusters: maxMaxClusters + 1}).cfg.MaxClusters; got != maxMaxClusters {
		t.Fatalf("oversized MaxClusters = %d, want hard cap %d", got, maxMaxClusters)
	}
}

func TestLoadClustersRetainsMostRecentWithinCap(t *testing.T) {
	miner := New(Config{MaxClusters: 2, SimTh: 0.9})
	base := time.Date(2026, 10, 9, 0, 0, 0, 0, time.UTC)
	miner.LoadClusters([]*LogCluster{
		{ID: 1, TemplateTokens: []string{"old", "record"}, LastSeen: base},
		{ID: 2, TemplateTokens: []string{"middle", "record"}, LastSeen: base.Add(time.Minute)},
		{ID: 3, TemplateTokens: []string{"new", "record"}, LastSeen: base.Add(2 * time.Minute)},
	})
	if got := len(miner.Clusters()); got != 2 {
		t.Fatalf("loaded %d clusters, want cap of 2", got)
	}
	if miner.ClusterByID(1) != nil || miner.ClusterByID(2) == nil || miner.ClusterByID(3) == nil {
		t.Fatal("load should evict the oldest cluster and retain the two newest")
	}
	miner.Process("fresh record")
	if got := len(miner.Clusters()); got != 2 {
		t.Fatalf("cluster count after insertion = %d, want 2", got)
	}
	if miner.ClusterByID(4) == nil {
		t.Fatal("next cluster ID should exceed IDs in discarded persisted state")
	}
}
