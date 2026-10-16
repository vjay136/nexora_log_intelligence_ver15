// Package projection implements field projection modes and count policies
package projection

import (
	"fmt"
	"sort"
)

// ProjectionMode defines how fields are filtered for output
type ProjectionMode string

const (
	ProjectionModeAll       ProjectionMode = "all"       // Include all fields
	ProjectionModeAllowlist ProjectionMode = "allowlist" // Include only allowed fields
	ProjectionModeDenylist  ProjectionMode = "denylist"  // Exclude denied fields
)

// CountPolicy defines field counting requirements
type CountPolicy string

const (
	CountPolicyExactSource  CountPolicy = "exact_source_fields" // Output must equal source fields
	CountPolicyAllowDerived CountPolicy = "allow_derived"       // Allow derived fields
	CountPolicyProjection   CountPolicy = "projection"          // Output must match projection
)

// FieldProjector handles field filtering and validation
type FieldProjector struct {
	Mode          ProjectionMode `json:"mode"`
	AllowedFields []string       `json:"allowed_fields,omitempty"`
	DeniedFields  []string       `json:"denied_fields,omitempty"`
	CountPolicy   CountPolicy    `json:"count_policy"`
}

// FieldMetadata tracks field source and derivation
type FieldMetadata struct {
	Name      string      `json:"name"`
	Value     interface{} `json:"value"`
	IsSource  bool        `json:"is_source"`          // Field came from input
	IsDerived bool        `json:"is_derived"`         // Field is derived/constant
	Evidence  string      `json:"evidence,omitempty"` // Input span this field covers
}

// ProjectionResult contains the filtered fields and metadata
type ProjectionResult struct {
	Fields            map[string]interface{} `json:"fields"`
	SourceFieldCount  int                    `json:"source_field_count"`
	DerivedFieldCount int                    `json:"derived_field_count"`
	TotalFieldCount   int                    `json:"total_field_count"`
	AllowedByPolicy   []string               `json:"allowed_by_policy,omitempty"`
	DeniedByPolicy    []string               `json:"denied_by_policy,omitempty"`
	Errors            []string               `json:"errors,omitempty"`
}

// NewFieldProjector creates a new field projector with validation
func NewFieldProjector(mode ProjectionMode, allowed, denied []string, policy CountPolicy) (*FieldProjector, error) {
	fp := &FieldProjector{
		Mode:          mode,
		AllowedFields: allowed,
		DeniedFields:  denied,
		CountPolicy:   policy,
	}

	// Validate configuration
	if mode == ProjectionModeAllowlist && len(allowed) == 0 {
		return nil, fmt.Errorf("allowlist mode requires allowed_fields")
	}
	if mode == ProjectionModeDenylist && len(denied) == 0 {
		return nil, fmt.Errorf("denylist mode requires denied_fields")
	}

	// Check for conflicts between allowed and denied lists
	if len(allowed) > 0 && len(denied) > 0 {
		allowedSet := make(map[string]bool)
		for _, field := range allowed {
			allowedSet[field] = true
		}
		for _, field := range denied {
			if allowedSet[field] {
				return nil, fmt.Errorf("field '%s' appears in both allowed and denied lists", field)
			}
		}
	}

	return fp, nil
}

// Project applies projection rules to field metadata and returns filtered output
func (fp *FieldProjector) Project(fields []FieldMetadata, expectedSourceCount int) (*ProjectionResult, error) {
	result := &ProjectionResult{
		Fields:          make(map[string]interface{}),
		AllowedByPolicy: make([]string, 0),
		DeniedByPolicy:  make([]string, 0),
		Errors:          make([]string, 0),
	}

	// Count source and derived fields
	sourceCount := 0
	derivedCount := 0
	for _, field := range fields {
		if field.IsSource {
			sourceCount++
		}
		if field.IsDerived {
			derivedCount++
		}
	}

	result.SourceFieldCount = sourceCount
	result.DerivedFieldCount = derivedCount

	// Apply projection filtering
	for _, field := range fields {
		include := fp.shouldIncludeField(field.Name)

		if include {
			result.Fields[field.Name] = field.Value
			result.AllowedByPolicy = append(result.AllowedByPolicy, field.Name)
		} else {
			result.DeniedByPolicy = append(result.DeniedByPolicy, field.Name)
		}
	}

	result.TotalFieldCount = len(result.Fields)

	// Validate count policy
	if err := fp.validateCountPolicy(result, expectedSourceCount, len(fields)); err != nil {
		result.Errors = append(result.Errors, err.Error())
	}

	// Sort field lists for deterministic output
	sort.Strings(result.AllowedByPolicy)
	sort.Strings(result.DeniedByPolicy)

	return result, nil
}

// shouldIncludeField determines if a field should be included based on projection mode
func (fp *FieldProjector) shouldIncludeField(fieldName string) bool {
	switch fp.Mode {
	case ProjectionModeAll:
		return true

	case ProjectionModeAllowlist:
		for _, allowed := range fp.AllowedFields {
			if allowed == fieldName {
				return true
			}
		}
		return false

	case ProjectionModeDenylist:
		for _, denied := range fp.DeniedFields {
			if denied == fieldName {
				return false
			}
		}
		return true

	default:
		return true
	}
}

// validateCountPolicy checks if the output satisfies the count policy
func (fp *FieldProjector) validateCountPolicy(result *ProjectionResult, expectedSourceCount, totalInputFields int) error {
	switch fp.CountPolicy {
	case CountPolicyExactSource:
		if result.SourceFieldCount != expectedSourceCount {
			return fmt.Errorf("exact_source_fields policy violation: got %d source fields, expected %d",
				result.SourceFieldCount, expectedSourceCount)
		}
		if result.TotalFieldCount != result.SourceFieldCount {
			return fmt.Errorf("exact_source_fields policy violation: output has %d fields but %d are source-backed",
				result.TotalFieldCount, result.SourceFieldCount)
		}

	case CountPolicyAllowDerived:
		if result.SourceFieldCount != expectedSourceCount {
			return fmt.Errorf("allow_derived policy violation: got %d source fields, expected %d",
				result.SourceFieldCount, expectedSourceCount)
		}
		// Derived fields are allowed, so total can exceed source count

	case CountPolicyProjection:
		// For allowlist mode, output count should match number of found allowed fields
		if fp.Mode == ProjectionModeAllowlist {
			foundAllowed := 0
			inputFieldNames := make(map[string]bool)
			for _, field := range result.AllowedByPolicy {
				inputFieldNames[field] = true
			}

			for _, allowed := range fp.AllowedFields {
				if inputFieldNames[allowed] {
					foundAllowed++
				}
			}

			if result.TotalFieldCount != foundAllowed {
				return fmt.Errorf("projection policy violation: expected %d allowed fields in output, got %d",
					foundAllowed, result.TotalFieldCount)
			}
		}
	}

	return nil
}

// ValidateExactFields ensures no extra fields are emitted beyond those configured
func ValidateExactFields(output map[string]interface{}, expected map[string]interface{}) error {
	// Check field count
	if len(output) != len(expected) {
		return fmt.Errorf("field count mismatch: output has %d fields, expected %d",
			len(output), len(expected))
	}

	// Check for extra fields
	for fieldName := range output {
		if _, exists := expected[fieldName]; !exists {
			return fmt.Errorf("unexpected field in output: %s", fieldName)
		}
	}

	// Check for missing fields
	for fieldName := range expected {
		if _, exists := output[fieldName]; !exists {
			return fmt.Errorf("missing field in output: %s", fieldName)
		}
	}

	return nil
}

// CreateFieldMetadata creates FieldMetadata from a simple field map
func CreateFieldMetadata(fields map[string]interface{}, sourceFields map[string]string) []FieldMetadata {
	metadata := make([]FieldMetadata, 0, len(fields))

	for name, value := range fields {
		evidence, isSource := sourceFields[name]
		metadata = append(metadata, FieldMetadata{
			Name:      name,
			Value:     value,
			IsSource:  isSource,
			IsDerived: !isSource,
			Evidence:  evidence,
		})
	}

	// Sort by field name for deterministic output
	sort.Slice(metadata, func(i, j int) bool {
		return metadata[i].Name < metadata[j].Name
	})

	return metadata
}
