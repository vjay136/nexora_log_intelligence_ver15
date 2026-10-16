// Package fleet manages the registry of all nli-agent client nodes.
package fleet

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"sync"
	"time"
)

// NodeStatus constants.
const (
	StatusOnline   = "online"
	StatusOffline  = "offline"
	StatusDegraded = "degraded" // parsing errors > threshold
)

// Node represents a registered nli-agent installation on a Linux host.
type Node struct {
	ID            string            `json:"id"`
	Hostname      string            `json:"hostname"`
	IP            string            `json:"ip"`
	OS            string            `json:"os"`
	Arch          string            `json:"arch"`
	Version       string            `json:"version"`
	ConfigHash    string            `json:"config_hash"`
	MetricsURL    string            `json:"metrics_url"`
	ControlURL    string            `json:"control_url"`
	Tags          map[string]string `json:"tags"`
	LastSeen      time.Time         `json:"last_seen"`
	RegisteredAt  time.Time         `json:"registered_at"`
	Status        string            `json:"status"`
	Metrics       *NodeMetrics      `json:"metrics,omitempty"`
	Issues        []string          `json:"issues,omitempty"`
	Parsers       []ParserStatus    `json:"parsers,omitempty"`
	ActiveRollout string            `json:"active_rollout,omitempty"`
	TokenHash     string            `json:"token_hash,omitempty"`
}

type ParserStatus struct {
	ID          string `json:"id"`
	Version     int    `json:"version"`
	Description string `json:"description,omitempty"`
	Source      string `json:"source,omitempty"`
	TestStatus  string `json:"test_status,omitempty"`
}

// NodeMetrics holds the latest counters from a node.
type NodeMetrics struct {
	Received       uint64    `json:"logs_received"`
	Parsed         uint64    `json:"logs_parsed"`
	Quarantined    uint64    `json:"logs_quarantined"`
	Errors         uint64    `json:"errors"`
	ParseRate      float64   `json:"parse_rate_pct"` // parsed/received * 100
	ParseLatencyMs float64   `json:"parse_latency_ms,omitempty"`
	CPUPercent     float64   `json:"cpu_percent,omitempty"`
	MemoryBytes    uint64    `json:"memory_bytes,omitempty"`
	MemoryPercent  float64   `json:"memory_percent,omitempty"`
	Load1          float64   `json:"load_1m,omitempty"`
	Load5          float64   `json:"load_5m,omitempty"`
	Load15         float64   `json:"load_15m,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// Fleet is the node registry.
type Fleet struct {
	mu    sync.RWMutex
	nodes map[string]*Node
	path  string
}

// New creates a Fleet, loading persisted state from path.
func New(path string) *Fleet {
	f := &Fleet{nodes: make(map[string]*Node), path: path}
	f.load()
	return f
}

func (f *Fleet) load() {
	data, err := os.ReadFile(f.path)
	if err != nil {
		return
	}
	var nodes []*Node
	if err := json.Unmarshal(data, &nodes); err != nil {
		return
	}
	for _, n := range nodes {
		f.nodes[n.ID] = n
	}
}

func (f *Fleet) save() {
	nodes := make([]*Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		nodes = append(nodes, n)
	}
	data, _ := json.MarshalIndent(nodes, "", "  ")
	tmp := f.path + ".tmp"
	os.WriteFile(tmp, data, 0640)
	os.Rename(tmp, f.path)
}

// Register adds or updates a node. Called when a client checks in.
func (f *Fleet) Register(n *Node) {
	f.mu.Lock()
	defer f.mu.Unlock()
	n.LastSeen = time.Now().UTC()
	if existing, ok := f.nodes[n.ID]; ok {
		n.RegisteredAt = existing.RegisteredAt
		if n.TokenHash == "" {
			n.TokenHash = existing.TokenHash
		}
	} else {
		n.RegisteredAt = time.Now().UTC()
	}
	n.Status = StatusOnline
	f.nodes[n.ID] = n
	f.save()
}

// Heartbeat updates last_seen and metrics for a node.
func (f *Fleet) Heartbeat(nodeID string, m *NodeMetrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	n, ok := f.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	n.LastSeen = time.Now().UTC()
	n.Status = StatusOnline
	if m != nil {
		m.UpdatedAt = time.Now().UTC()
		if m.Received > 0 {
			m.ParseRate = float64(m.Parsed) / float64(m.Received) * 100
		}
		n.Metrics = m
		if m.Errors > 100 {
			n.Status = StatusDegraded
		}
	}
	f.save()
	return nil
}

// MarkStale marks nodes that haven't checked in within timeout as offline.
func (f *Fleet) MarkStale(timeout time.Duration) {
	f.mu.Lock()
	defer f.mu.Unlock()
	changed := false
	for _, n := range f.nodes {
		if n.Status == StatusOnline && time.Since(n.LastSeen) > timeout {
			n.Status = StatusOffline
			changed = true
		}
	}
	if changed {
		f.save()
	}
}

// All returns a snapshot of all nodes.
func (f *Fleet) All() []*Node {
	f.mu.RLock()
	defer f.mu.RUnlock()
	out := make([]*Node, 0, len(f.nodes))
	for _, n := range f.nodes {
		cp := *n
		cp.TokenHash = ""
		cp.Tags = cloneTags(n.Tags)
		cp.Issues = append([]string(nil), n.Issues...)
		cp.Parsers = append([]ParserStatus(nil), n.Parsers...)
		out = append(out, &cp)
	}
	return out
}

// Get returns a node by ID.
func (f *Fleet) Get(id string) (*Node, bool) {
	f.mu.RLock()
	defer f.mu.RUnlock()
	n, ok := f.nodes[id]
	if !ok {
		return nil, false
	}
	cp := *n
	cp.TokenHash = ""
	cp.Tags = cloneTags(n.Tags)
	cp.Issues = append([]string(nil), n.Issues...)
	cp.Parsers = append([]ParserStatus(nil), n.Parsers...)
	return &cp, true
}

// Stats returns fleet-wide aggregate statistics.
func (f *Fleet) Stats() map[string]interface{} {
	f.mu.RLock()
	defer f.mu.RUnlock()
	total, online, offline, degraded := 0, 0, 0, 0
	var totalReceived, totalParsed uint64
	for _, n := range f.nodes {
		total++
		switch n.Status {
		case StatusOnline:
			online++
		case StatusOffline:
			offline++
		case StatusDegraded:
			degraded++
		}
		if n.Metrics != nil {
			totalReceived += n.Metrics.Received
			totalParsed += n.Metrics.Parsed
		}
	}
	parseRate := 0.0
	if totalReceived > 0 {
		parseRate = float64(totalParsed) / float64(totalReceived) * 100
	}
	return map[string]interface{}{
		"total": total, "online": online, "offline": offline, "degraded": degraded,
		"total_logs_received": totalReceived,
		"total_logs_parsed":   totalParsed,
		"fleet_parse_rate":    parseRate,
	}
}

// SetToken stores only a SHA-256 digest of a node credential.
func (f *Fleet) SetToken(nodeID, token string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	node, ok := f.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	digest := sha256.Sum256([]byte(token))
	node.TokenHash = hex.EncodeToString(digest[:])
	f.save()
	return nil
}

// Authorize verifies a node credential without exposing stored digests.
func (f *Fleet) Authorize(nodeID, token string) bool {
	if token == "" {
		return false
	}
	digest := sha256.Sum256([]byte(token))
	candidate := hex.EncodeToString(digest[:])
	f.mu.RLock()
	defer f.mu.RUnlock()
	node, ok := f.nodes[nodeID]
	return ok && node.TokenHash != "" && subtle.ConstantTimeCompare([]byte(node.TokenHash), []byte(candidate)) == 1
}

// UpdateHeartbeat records runtime state and parser inventory from an agent.
func (f *Fleet) UpdateHeartbeat(nodeID, version, configHash, health string, issues []string, parsers []ParserStatus, metrics *NodeMetrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	node, ok := f.nodes[nodeID]
	if !ok {
		return fmt.Errorf("node not found: %s", nodeID)
	}
	node.LastSeen = time.Now().UTC()
	node.Version = version
	node.ConfigHash = configHash
	node.Issues = append([]string(nil), issues...)
	node.Parsers = append([]ParserStatus(nil), parsers...)
	node.Status = StatusOnline
	if health == StatusDegraded || len(issues) > 0 {
		node.Status = StatusDegraded
	}
	if metrics != nil {
		metrics.UpdatedAt = time.Now().UTC()
		if metrics.Received > 0 {
			metrics.ParseRate = float64(metrics.Parsed) / float64(metrics.Received) * 100
		}
		node.Metrics = metrics
		if metrics.Errors > 100 {
			node.Status = StatusDegraded
		}
	}
	f.save()
	return nil
}

func cloneTags(tags map[string]string) map[string]string {
	if tags == nil {
		return nil
	}
	copyOfTags := make(map[string]string, len(tags))
	for key, value := range tags {
		copyOfTags[key] = value
	}
	return copyOfTags
}
