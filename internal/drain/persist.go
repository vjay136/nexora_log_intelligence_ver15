// Package drain - persistence for template clusters.
package drain

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const persistVersion = 1

type persistState struct {
	Version  int           `json:"version"`
	Clusters []*LogCluster `json:"clusters"`
}

// Persister manages periodic save/load of Drain cluster state.
type Persister struct {
	path     string
	miner    *Miner
	mu       sync.Mutex
	lastSave time.Time
	dirty    int
}

// NewPersister creates a persister for the given miner and state path.
func NewPersister(miner *Miner, stateDir string) *Persister {
	return &Persister{
		path:  filepath.Join(stateDir, "templates.json"),
		miner: miner,
	}
}

// Load restores clusters from disk. Silently succeeds if file doesn't exist.
func (p *Persister) Load() error {
	data, err := os.ReadFile(p.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	var state persistState
	if err := json.Unmarshal(data, &state); err != nil {
		return err
	}
	if state.Version != persistVersion {
		return nil // ignore incompatible state
	}
	p.miner.LoadClusters(state.Clusters)
	return nil
}

// Mark marks that new data has arrived (increments dirty counter).
func (p *Persister) Mark() {
	p.mu.Lock()
	p.dirty++
	p.mu.Unlock()
}

// SaveIfDue saves to disk if ≥1000 new logs or ≥60s since last save.
func (p *Persister) SaveIfDue() error {
	p.mu.Lock()
	due := p.dirty >= 1000 || (!p.lastSave.IsZero() && time.Since(p.lastSave) >= 60*time.Second)
	p.mu.Unlock()
	if !due {
		return nil
	}
	return p.Save()
}

// Save writes cluster state to disk atomically.
func (p *Persister) Save() error {
	clusters := p.miner.Clusters()
	state := persistState{Version: persistVersion, Clusters: clusters}
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}
	tmp := p.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0640); err != nil {
		return err
	}
	if err := os.Rename(tmp, p.path); err != nil {
		return err
	}
	p.mu.Lock()
	p.dirty = 0
	p.lastSave = time.Now()
	p.mu.Unlock()
	return nil
}
