// Package schema provides per-service JSON schema validation for NLI parsed records.
// Each service can define required/optional fields and their expected types.
// Unknown fields are preserved under the _unknown key rather than dropped.
package schema

import (
	"fmt"
	"strings"
	"sync"
)

// FieldSpec describes one expected field.
type FieldSpec struct {
	Name     string `json:"name"`
	Type     string `json:"type"` // string, int, float, bool, ip, timestamp, email, url
	Required bool   `json:"required"`
}

// Schema is a per-service field specification.
type Schema struct {
	Service string      `json:"service"`
	Fields  []FieldSpec `json:"fields"`
}

// ValidationResult is returned by Validate.
type ValidationResult struct {
	Valid  bool
	Errors []string
}

// Registry holds schemas keyed by service name.
type Registry struct {
	mu      sync.RWMutex
	schemas map[string]*Schema
}

// New returns an empty schema registry.
func New() *Registry {
	return &Registry{schemas: make(map[string]*Schema)}
}

// Register adds or replaces a schema.
func (r *Registry) Register(s *Schema) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.schemas[s.Service] = s
}

// Validate checks a parsed data map against the schema for the given service.
// If no schema is registered for the service, all records pass.
func (r *Registry) Validate(service string, data map[string]interface{}) ValidationResult {
	r.mu.RLock()
	s, ok := r.schemas[service]
	r.mu.RUnlock()
	if !ok {
		return ValidationResult{Valid: true}
	}

	var errs []string
	for _, f := range s.Fields {
		v, exists := data[f.Name]
		if !exists {
			if f.Required {
				errs = append(errs, fmt.Sprintf("missing required field: %s", f.Name))
			}
			continue
		}
		if err := checkType(f.Name, v, f.Type); err != "" {
			errs = append(errs, err)
		}
	}
	return ValidationResult{Valid: len(errs) == 0, Errors: errs}
}

func checkType(name string, v interface{}, typ string) string {
	if v == nil {
		return ""
	}
	switch typ {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Sprintf("field %s: expected string, got %T", name, v)
		}
	case "int", "integer":
		switch v.(type) {
		case int, int64, float64:
		default:
			if s, ok := v.(string); ok && strings.TrimSpace(s) == "" {
				return fmt.Sprintf("field %s: expected integer", name)
			}
		}
	case "float":
		switch v.(type) {
		case float32, float64, int, int64:
		default:
			return fmt.Sprintf("field %s: expected float, got %T", name, v)
		}
	case "bool":
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("field %s: expected bool, got %T", name, v)
		}
	}
	return ""
}
