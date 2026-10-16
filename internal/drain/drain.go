// Package drain implements a Drain3-style online log template miner.
// Algorithm: "Drain: An Online Log Parsing Approach with Fixed Depth Tree"
// (He et al., ICWS 2017). Re-implemented from the algorithm description;
// no GPL code copied.
package drain

import (
	"sort"
	"strings"
	"sync"
	"time"
)

const wildcard = "<*>"

const (
	defaultMaxClusters = 10_000
	maxMaxClusters     = 100_000
)

// Config holds Drain algorithm parameters.
type Config struct {
	Depth       int     `json:"depth"`
	SimTh       float64 `json:"sim_th"`
	MaxChildren int     `json:"max_children"`
	MaxClusters int     `json:"max_clusters"`
}

func (c *Config) setDefaults() {
	if c.Depth < 3 {
		c.Depth = 4
	}
	if c.SimTh <= 0 {
		c.SimTh = 0.5
	}
	if c.MaxChildren <= 0 {
		c.MaxChildren = 100
	}
	if c.MaxClusters <= 0 {
		c.MaxClusters = defaultMaxClusters
	} else if c.MaxClusters > maxMaxClusters {
		c.MaxClusters = maxMaxClusters
	}
}

// LogCluster is a leaf node in the Drain tree representing a log template.
type LogCluster struct {
	ID             int       `json:"id"`
	TemplateTokens []string  `json:"template"`
	Size           int       `json:"size"`
	ParserID       string    `json:"parser_id,omitempty"`
	LastSeen       time.Time `json:"last_seen"`
}

// Template returns the template as a space-joined string.
func (c *LogCluster) Template() string {
	return strings.Join(c.TemplateTokens, " ")
}

// node is an internal tree node.
type node struct {
	children map[string]*node
	clusters []*LogCluster
}

func newNode() *node {
	return &node{children: make(map[string]*node)}
}

// Miner is the Drain tree.
type Miner struct {
	cfg      Config
	root     *node // children keyed by string(token_count)
	clusters map[int]*LogCluster
	lruOrder []int // cluster IDs in LRU order (oldest first)
	nextID   int
	mu       sync.RWMutex
}

// Match result returned by Process.
type Match struct {
	ClusterID int
	IsNew     bool
	Template  string
	ParserID  string
}

// New creates a Miner with the given config.
func New(cfg Config) *Miner {
	cfg.setDefaults()
	return &Miner{
		cfg:      cfg,
		root:     newNode(),
		clusters: make(map[int]*LogCluster),
		nextID:   1,
	}
}

// Process mines a masked log line, updating or creating a cluster.
func (m *Miner) Process(masked string) Match {
	tokens := strings.Fields(masked)
	if len(tokens) == 0 {
		return Match{}
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	cluster := m.treeSearch(tokens)
	isNew := cluster == nil

	if isNew {
		cluster = &LogCluster{
			ID:             m.nextID,
			TemplateTokens: make([]string, len(tokens)),
			Size:           1,
			LastSeen:       time.Now(),
		}
		copy(cluster.TemplateTokens, tokens)
		m.nextID++
		m.addToTree(cluster)
		m.clusters[cluster.ID] = cluster
		m.lruTouch(cluster.ID)
		if m.cfg.MaxClusters > 0 && len(m.clusters) > m.cfg.MaxClusters {
			m.evictLRU()
		}
	} else {
		cluster.TemplateTokens = updateTemplate(cluster.TemplateTokens, tokens)
		cluster.Size++
		cluster.LastSeen = time.Now()
		m.lruTouch(cluster.ID)
	}

	return Match{
		ClusterID: cluster.ID,
		IsNew:     isNew,
		Template:  cluster.Template(),
		ParserID:  cluster.ParserID,
	}
}

// SetParserID assigns a parser to a cluster (called by registry after AI discovery).
func (m *Miner) SetParserID(clusterID int, parserID string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if c, ok := m.clusters[clusterID]; ok {
		c.ParserID = parserID
	}
}

// Clusters returns a snapshot of all clusters (for persistence).
func (m *Miner) Clusters() []*LogCluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	out := make([]*LogCluster, 0, len(m.clusters))
	for _, c := range m.clusters {
		cp := *c
		cp.TemplateTokens = make([]string, len(c.TemplateTokens))
		copy(cp.TemplateTokens, c.TemplateTokens)
		out = append(out, &cp)
	}
	return out
}

// LoadClusters restores clusters from persistence (rebuilds tree).
func (m *Miner) LoadClusters(clusters []*LogCluster) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.root = newNode()
	m.clusters = make(map[int]*LogCluster)
	m.lruOrder = nil
	maxID := 0
	byID := make(map[int]*LogCluster, len(clusters))
	for _, c := range clusters {
		if c == nil {
			continue
		}
		if c.ID > maxID {
			maxID = c.ID
		}
		previous := byID[c.ID]
		if previous == nil || !c.LastSeen.Before(previous.LastSeen) {
			byID[c.ID] = c
		}
	}
	clusters = make([]*LogCluster, 0, len(byID))
	for _, c := range byID {
		clusters = append(clusters, c)
	}
	sort.Slice(clusters, func(i, j int) bool {
		if clusters[i].LastSeen.Equal(clusters[j].LastSeen) {
			return clusters[i].ID < clusters[j].ID
		}
		return clusters[i].LastSeen.Before(clusters[j].LastSeen)
	})
	// Persisted state may have been created with an older unlimited setting.
	// Keep the most recently seen bounded set before rebuilding the tree.
	if len(clusters) > m.cfg.MaxClusters {
		clusters = clusters[len(clusters)-m.cfg.MaxClusters:]
	}
	for _, c := range clusters {
		m.clusters[c.ID] = c
		m.addToTree(c)
		m.lruOrder = append(m.lruOrder, c.ID)
	}
	m.nextID = maxID + 1
}

// ClusterByID returns a cluster by ID (nil if not found).
func (m *Miner) ClusterByID(id int) *LogCluster {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.clusters[id]
}

// --- tree internals ---

func (m *Miner) treeSearch(tokens []string) *LogCluster {
	lenNode, ok := m.root.children[lenKey(len(tokens))]
	if !ok {
		return nil
	}

	if exact := lenNode.children[tokens[0]]; exact != nil {
		if cluster := m.bestMatch(exact, tokens); cluster != nil {
			return cluster
		}
	}
	if wildcardNode := lenNode.children[wildcard]; wildcardNode != nil {
		return m.bestMatch(wildcardNode, tokens)
	}
	return nil
}

func (m *Miner) bestMatch(n *node, tokens []string) *LogCluster {
	var best *LogCluster
	bestSimilarity := -1.0
	for _, cluster := range n.clusters {
		current := similarity(cluster.TemplateTokens, tokens)
		if current > bestSimilarity {
			bestSimilarity = current
			best = cluster
		}
	}
	if bestSimilarity >= m.cfg.SimTh {
		return best
	}
	return nil
}

func (m *Miner) addToTree(c *LogCluster) {
	tokens := c.TemplateTokens
	lengthKey := lenKey(len(tokens))
	lengthNode, ok := m.root.children[lengthKey]
	if !ok {
		lengthNode = newNode()
		m.root.children[lengthKey] = lengthNode
	}

	firstToken := tokens[0]
	if _, exists := lengthNode.children[firstToken]; !exists && len(lengthNode.children) >= m.cfg.MaxChildren {
		firstToken = wildcard
	}
	leaf, ok := lengthNode.children[firstToken]
	if !ok {
		leaf = newNode()
		lengthNode.children[firstToken] = leaf
	}
	for _, existing := range leaf.clusters {
		if existing.ID == c.ID {
			return
		}
	}
	leaf.clusters = append(leaf.clusters, c)
}

func (m *Miner) lruTouch(id int) {
	// Remove from current position, append to end (most recently used).
	for i, v := range m.lruOrder {
		if v == id {
			m.lruOrder = append(m.lruOrder[:i], m.lruOrder[i+1:]...)
			break
		}
	}
	m.lruOrder = append(m.lruOrder, id)
}

func (m *Miner) evictLRU() {
	if len(m.lruOrder) == 0 {
		return
	}
	evictID := m.lruOrder[0]
	m.lruOrder = m.lruOrder[1:]
	delete(m.clusters, evictID)
	m.root = newNode()
	for _, cluster := range m.clusters {
		m.addToTree(cluster)
	}
}

// --- algorithm helpers ---

func similarity(template, tokens []string) float64 {
	if len(template) != len(tokens) {
		return 0
	}
	matched := 0
	for i, t := range template {
		if t == wildcard || t == tokens[i] {
			matched++
		}
	}
	return float64(matched) / float64(len(template))
}

func updateTemplate(template, tokens []string) []string {
	out := make([]string, len(template))
	for i, t := range template {
		if t == wildcard || t == tokens[i] {
			out[i] = t
		} else {
			out[i] = wildcard
		}
	}
	return out
}

func lenKey(n int) string {
	return "L" + strings.Repeat("0", max(0, 4-len(itoa(n)))) + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	buf := make([]byte, 0, 10)
	for n > 0 {
		buf = append([]byte{byte('0' + n%10)}, buf...)
		n /= 10
	}
	return string(buf)
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
