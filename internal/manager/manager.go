// Package manager implements the NLI multi-client management HTTP API.
package manager

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"time"
)

// ClientInfo holds a registered nli-agent instance's metadata.
type ClientInfo struct {
	Host       string    `json:"host"`
	PID        int       `json:"pid"`
	Version    string    `json:"version"`
	ConfigHash string    `json:"config_hash"`
	MetricsURL string    `json:"metrics_url"`
	LastSeen   time.Time `json:"last_seen"`
	Online     bool      `json:"online"`
}

// Registry holds all known nli-agent clients.
type Registry struct {
	mu      sync.RWMutex
	clients map[string]*ClientInfo // keyed by host
}

// NewRegistry creates an empty client registry.
func NewRegistry() *Registry {
	return &Registry{clients: make(map[string]*ClientInfo)}
}

// Register adds or updates a client.
func (r *Registry) Register(c *ClientInfo) {
	c.LastSeen = time.Now()
	c.Online = true
	r.mu.Lock()
	r.clients[c.Host] = c
	r.mu.Unlock()
}

// All returns a snapshot of all clients, marking stale ones offline.
func (r *Registry) All() []*ClientInfo {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := make([]*ClientInfo, 0, len(r.clients))
	for _, c := range r.clients {
		cp := *c
		if time.Since(c.LastSeen) > 90*time.Second {
			cp.Online = false
		}
		out = append(out, &cp)
	}
	return out
}

// Get returns a client by host (nil if not found).
func (r *Registry) Get(host string) *ClientInfo {
	r.mu.RLock()
	defer r.mu.RUnlock()
	c := r.clients[host]
	if c == nil {
		return nil
	}
	cp := *c
	return &cp
}

// Config for the manager server.
type Config struct {
	ListenAddress string `json:"listen_address"` // default 127.0.0.1:12381
}

// Server is the nli-manager HTTP server.
type Server struct {
	cfg      Config
	registry *Registry
	mux      *http.ServeMux
}

// New creates a manager server.
func New(cfg Config) *Server {
	if cfg.ListenAddress == "" {
		cfg.ListenAddress = "127.0.0.1:12381"
	}
	s := &Server{cfg: cfg, registry: NewRegistry()}
	s.mux = http.NewServeMux()
	s.routes()
	return s
}

func (s *Server) routes() {
	s.mux.HandleFunc("/api/v1/health", s.handleHealth)
	s.mux.HandleFunc("/api/v1/clients", s.handleClients)
	s.mux.HandleFunc("/api/v1/clients/", s.handleClient)
	s.mux.HandleFunc("/api/v1/register", s.handleRegister)
	s.mux.Handle("/", http.FileServer(http.Dir("/usr/share/nli/web")))
}

// ListenAndServe starts the manager server.
func (s *Server) ListenAndServe() error {
	srv := &http.Server{
		Addr:              s.cfg.ListenAddress,
		Handler:           s.mux,
		ReadHeaderTimeout: 5 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}
	fmt.Printf("nli-manager listening on %s\n", s.cfg.ListenAddress)
	return srv.ListenAndServe()
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]interface{}{
		"status":  "running",
		"clients": len(s.registry.All()),
	})
}

func (s *Server) handleClients(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(s.registry.All())
}

func (s *Server) handleClient(w http.ResponseWriter, r *http.Request) {
	host := r.URL.Path[len("/api/v1/clients/"):]
	c := s.registry.Get(host)
	if c == nil {
		http.Error(w, "client not found", http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(c)
}

func (s *Server) handleRegister(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "POST required", http.StatusMethodNotAllowed)
		return
	}
	var c ClientInfo
	if err := json.NewDecoder(r.Body).Decode(&c); err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	s.registry.Register(&c)
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"status": "registered"})
}
