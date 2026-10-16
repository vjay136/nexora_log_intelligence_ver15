package projection

import (
	"testing"
)

func TestFieldProjector_AllMode(t *testing.T) {
	projector, err := NewFieldProjector(ProjectionModeAll, nil, nil, CountPolicyAllowDerived)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "derived1", Value: "constant", IsDerived: true},
	}

	result, err := projector.Project(fields, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	// All mode should include everything
	if len(result.Fields) != 3 {
		t.Errorf("Field count: got %d, expected 3", len(result.Fields))
	}

	if result.SourceFieldCount != 2 {
		t.Errorf("Source field count: got %d, expected 2", result.SourceFieldCount)
	}

	if result.DerivedFieldCount != 1 {
		t.Errorf("Derived field count: got %d, expected 1", result.DerivedFieldCount)
	}

	// Check all fields are present
	expectedFields := []string{"field1", "field2", "derived1"}
	for _, fieldName := range expectedFields {
		if _, exists := result.Fields[fieldName]; !exists {
			t.Errorf("Missing field: %s", fieldName)
		}
	}
}

func TestFieldProjector_AllowlistMode(t *testing.T) {
	allowedFields := []string{"field1", "derived1"}
	projector, err := NewFieldProjector(ProjectionModeAllowlist, allowedFields, nil, CountPolicyProjection)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "derived1", Value: "constant", IsDerived: true},
		{Name: "derived2", Value: "other", IsDerived: true},
	}

	result, err := projector.Project(fields, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	// Only allowed fields should be present
	if len(result.Fields) != 2 {
		t.Errorf("Field count: got %d, expected 2", len(result.Fields))
	}

	if _, exists := result.Fields["field1"]; !exists {
		t.Error("Missing allowed field: field1")
	}
	if _, exists := result.Fields["derived1"]; !exists {
		t.Error("Missing allowed field: derived1")
	}
	if _, exists := result.Fields["field2"]; exists {
		t.Error("Unexpected denied field: field2")
	}

	// Check denied fields list
	expectedDenied := []string{"derived2", "field2"}
	if len(result.DeniedByPolicy) != len(expectedDenied) {
		t.Errorf("Denied fields count: got %d, expected %d", len(result.DeniedByPolicy), len(expectedDenied))
	}
}

func TestFieldProjector_DenylistMode(t *testing.T) {
	deniedFields := []string{"field2", "derived2"}
	projector, err := NewFieldProjector(ProjectionModeDenylist, nil, deniedFields, CountPolicyAllowDerived)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "derived1", Value: "constant", IsDerived: true},
		{Name: "derived2", Value: "other", IsDerived: true},
	}

	result, err := projector.Project(fields, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	// Should include everything except denied fields
	if len(result.Fields) != 2 {
		t.Errorf("Field count: got %d, expected 2", len(result.Fields))
	}

	if _, exists := result.Fields["field1"]; !exists {
		t.Error("Missing allowed field: field1")
	}
	if _, exists := result.Fields["derived1"]; !exists {
		t.Error("Missing allowed field: derived1")
	}
	if _, exists := result.Fields["field2"]; exists {
		t.Error("Unexpected denied field: field2")
	}
	if _, exists := result.Fields["derived2"]; exists {
		t.Error("Unexpected denied field: derived2")
	}
}

func TestFieldProjector_ExactSourceFieldsPolicy(t *testing.T) {
	projector, err := NewFieldProjector(ProjectionModeAll, nil, nil, CountPolicyExactSource)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	// Test case where policy should pass
	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
	}

	result, err := projector.Project(fields, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Unexpected policy errors: %v", result.Errors)
	}

	// Test case where policy should fail due to wrong source count
	fieldsWithDerived := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "derived1", Value: "constant", IsDerived: true},
	}

	result, err = projector.Project(fieldsWithDerived, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	if len(result.Errors) == 0 {
		t.Error("Expected policy error for derived fields in exact_source_fields mode")
	}
}

func TestFieldProjector_AllowDerivedPolicy(t *testing.T) {
	projector, err := NewFieldProjector(ProjectionModeAll, nil, nil, CountPolicyAllowDerived)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "derived1", Value: "constant", IsDerived: true},
		{Name: "derived2", Value: "other", IsDerived: true},
	}

	result, err := projector.Project(fields, 2)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	// Should pass validation with derived fields
	if len(result.Errors) != 0 {
		t.Errorf("Unexpected policy errors: %v", result.Errors)
	}

	if result.SourceFieldCount != 2 {
		t.Errorf("Source field count: got %d, expected 2", result.SourceFieldCount)
	}

	if result.DerivedFieldCount != 2 {
		t.Errorf("Derived field count: got %d, expected 2", result.DerivedFieldCount)
	}

	if result.TotalFieldCount != 4 {
		t.Errorf("Total field count: got %d, expected 4", result.TotalFieldCount)
	}
}

func TestFieldProjector_ProjectionPolicy(t *testing.T) {
	allowedFields := []string{"field1", "field3"}
	projector, err := NewFieldProjector(ProjectionModeAllowlist, allowedFields, nil, CountPolicyProjection)
	if err != nil {
		t.Fatalf("Failed to create projector: %v", err)
	}

	fields := []FieldMetadata{
		{Name: "field1", Value: "value1", IsSource: true},
		{Name: "field2", Value: "value2", IsSource: true},
		{Name: "field3", Value: "value3", IsSource: true},
	}

	result, err := projector.Project(fields, 3)
	if err != nil {
		t.Fatalf("Project failed: %v", err)
	}

	// Should include only the 2 allowed fields that exist
	if result.TotalFieldCount != 2 {
		t.Errorf("Total field count: got %d, expected 2", result.TotalFieldCount)
	}

	if len(result.Errors) != 0 {
		t.Errorf("Unexpected projection policy errors: %v", result.Errors)
	}
}

func TestFieldProjector_DeniedAndAllowedConflict(t *testing.T) {
	allowedFields := []string{"field1", "field2"}
	deniedFields := []string{"field1", "field3"} // field1 conflicts

	_, err := NewFieldProjector(ProjectionModeAllowlist, allowedFields, deniedFields, CountPolicyAllowDerived)
	if err == nil {
		t.Error("Expected error for conflicting allowed/denied fields")
	}

	if !containsSubstring(err.Error(), "appears in both") {
		t.Errorf("Error should mention conflict: %v", err)
	}
}

func TestFieldProjector_InvalidConfigurations(t *testing.T) {
	// Allowlist mode without allowed fields
	_, err := NewFieldProjector(ProjectionModeAllowlist, nil, nil, CountPolicyAllowDerived)
	if err == nil {
		t.Error("Expected error for allowlist mode without allowed_fields")
	}

	// Denylist mode without denied fields
	_, err = NewFieldProjector(ProjectionModeDenylist, nil, nil, CountPolicyAllowDerived)
	if err == nil {
		t.Error("Expected error for denylist mode without denied_fields")
	}
}

func TestCreateFieldMetadata(t *testing.T) {
	fields := map[string]interface{}{
		"source1":  "value1",
		"source2":  "value2",
		"derived1": "constant",
	}

	sourceFields := map[string]string{
		"source1": "input_span_1",
		"source2": "input_span_2",
		// derived1 not in source map
	}

	metadata := CreateFieldMetadata(fields, sourceFields)

	if len(metadata) != 3 {
		t.Errorf("Metadata count: got %d, expected 3", len(metadata))
	}

	// Check source fields
	var source1Meta, derived1Meta *FieldMetadata
	for i := range metadata {
		if metadata[i].Name == "source1" {
			source1Meta = &metadata[i]
		}
		if metadata[i].Name == "derived1" {
			derived1Meta = &metadata[i]
		}
	}

	if source1Meta == nil {
		t.Fatal("Missing source1 metadata")
	}
	if !source1Meta.IsSource {
		t.Error("source1 should be marked as source")
	}
	if source1Meta.IsDerived {
		t.Error("source1 should not be marked as derived")
	}
	if source1Meta.Evidence != "input_span_1" {
		t.Errorf("source1 evidence: got %s, expected 'input_span_1'", source1Meta.Evidence)
	}

	if derived1Meta == nil {
		t.Fatal("Missing derived1 metadata")
	}
	if derived1Meta.IsSource {
		t.Error("derived1 should not be marked as source")
	}
	if !derived1Meta.IsDerived {
		t.Error("derived1 should be marked as derived")
	}
}

func TestValidateExactFields(t *testing.T) {
	expected := map[string]interface{}{
		"field1": "value1",
		"field2": 42,
		"field3": true,
	}

	// Test exact match
	output := map[string]interface{}{
		"field1": "value1",
		"field2": 42,
		"field3": true,
	}

	err := ValidateExactFields(output, expected)
	if err != nil {
		t.Errorf("Exact match should pass: %v", err)
	}

	// Test missing field
	outputMissing := map[string]interface{}{
		"field1": "value1",
		"field2": 42,
		// field3 missing
	}

	err = ValidateExactFields(outputMissing, expected)
	if err == nil {
		t.Error("Expected error for missing field")
	}

	// Test extra field
	outputExtra := map[string]interface{}{
		"field1": "value1",
		"field2": 42,
		"field3": true,
		"extra":  "unexpected",
	}

	err = ValidateExactFields(outputExtra, expected)
	if err == nil {
		t.Error("Expected error for extra field")
	}
}

// Helper function for substring checking
func containsSubstring(s, substr string) bool {
	return len(s) >= len(substr) &&
		func() bool {
			for i := 0; i <= len(s)-len(substr); i++ {
				if s[i:i+len(substr)] == substr {
					return true
				}
			}
			return false
		}()
}
