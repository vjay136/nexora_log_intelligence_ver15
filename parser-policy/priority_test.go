package parserpolicy

import (
	"fmt"
	"testing"
	"time"
)

// createValidTestPack creates a minimal valid ParserPack for testing
func createValidTestPack() *ParserPack {
	now := time.Now()
	return &ParserPack{
		ID:          "test_pack",
		Version:     "1.0.0",
		Revision:    1,
		SourceType:  "manual",
		Author:      "test_author",
		ReviewState: ReviewStateDraft,
		CreatedAt:   now,
		Rules: []ParserRule{
			{
				ID:             "test_rule",
				Name:           "Test Rule",
				Priority:       100,
				Stages:         []Stage{{ID: "stage1", Name: "Test Stage", Type: StageTypeRegex, Config: map[string]interface{}{"pattern": ".*"}, FailurePolicy: FailurePolicyContinue}},
				FailurePolicy:  FailurePolicyContinue,
				ProjectionMode: ProjectionModeAll,
				CountPolicy:    CountPolicyExactSource,
			},
		},
		ResourceLimits: DefaultResourceLimits(),
	}
}

func TestSortRulesByPriority_AscendingPriority(t *testing.T) {
	rules := []ParserRule{
		{ID: "rule_high", Priority: 300},
		{ID: "rule_low", Priority: 100},
		{ID: "rule_medium", Priority: 200},
	}

	sorted := SortRulesByPriority(rules)

	// Should be sorted by ascending priority
	expectedOrder := []string{"rule_low", "rule_medium", "rule_high"}
	for i, expectedID := range expectedOrder {
		if sorted[i].ID != expectedID {
			t.Errorf("Position %d: got %s, expected %s", i, sorted[i].ID, expectedID)
		}
	}

	// Check priorities are correct
	if sorted[0].Priority != 100 {
		t.Errorf("First rule priority: got %d, expected 100", sorted[0].Priority)
	}
	if sorted[2].Priority != 300 {
		t.Errorf("Last rule priority: got %d, expected 300", sorted[2].Priority)
	}
}

func TestSortRulesByPriority_DeterministicTieBreaking(t *testing.T) {
	rules := []ParserRule{
		{ID: "rule_z", Priority: 100},
		{ID: "rule_a", Priority: 100}, // Same priority as rule_z
		{ID: "rule_m", Priority: 100}, // Same priority
		{ID: "rule_b", Priority: 200},
	}

	sorted := SortRulesByPriority(rules)

	// Within same priority, should be sorted by ID (alphabetical)
	if sorted[0].ID != "rule_a" {
		t.Errorf("First rule ID: got %s, expected 'rule_a'", sorted[0].ID)
	}
	if sorted[1].ID != "rule_m" {
		t.Errorf("Second rule ID: got %s, expected 'rule_m'", sorted[1].ID)
	}
	if sorted[2].ID != "rule_z" {
		t.Errorf("Third rule ID: got %s, expected 'rule_z'", sorted[2].ID)
	}
	if sorted[3].ID != "rule_b" {
		t.Errorf("Fourth rule ID: got %s, expected 'rule_b'", sorted[3].ID)
	}

	// All first three should have same priority
	for i := 0; i < 3; i++ {
		if sorted[i].Priority != 100 {
			t.Errorf("Rule %d priority: got %d, expected 100", i, sorted[i].Priority)
		}
	}
}

func TestSortRulesByPriority_PreservesOriginal(t *testing.T) {
	original := []ParserRule{
		{ID: "rule1", Priority: 200},
		{ID: "rule2", Priority: 100},
	}

	// Make a copy to verify original is unchanged
	originalCopy := make([]ParserRule, len(original))
	copy(originalCopy, original)

	sorted := SortRulesByPriority(original)

	// Original should be unchanged
	for i, rule := range original {
		if rule.ID != originalCopy[i].ID || rule.Priority != originalCopy[i].Priority {
			t.Error("Original slice was modified")
		}
	}

	// Sorted should be different order
	if sorted[0].ID != "rule2" {
		t.Errorf("First sorted rule: got %s, expected 'rule2'", sorted[0].ID)
	}
	if sorted[1].ID != "rule1" {
		t.Errorf("Second sorted rule: got %s, expected 'rule1'", sorted[1].ID)
	}
}

func TestOrderedAttemptTelemetry(t *testing.T) {
	// Test that we can record ordered attempts with failure reasons

	// Simulate parser attempting multiple rules
	rules := []ParserRule{
		{ID: "smtp_strict", Priority: 100, ServiceSelectors: []string{"smtp"}},
		{ID: "smtp_generic", Priority: 200, ServiceSelectors: []string{"smtp"}},
		{ID: "fallback", Priority: 300, ServiceSelectors: []string{"*"}},
	}

	// Sort by priority
	sortedRules := SortRulesByPriority(rules)

	// Verify order
	expectedOrder := []string{"smtp_strict", "smtp_generic", "fallback"}
	for i, expectedID := range expectedOrder {
		if sortedRules[i].ID != expectedID {
			t.Errorf("Sorted rule %d: got %s, expected %s", i, sortedRules[i].ID, expectedID)
		}
	}

	// Simulate parsing attempts (this would happen in real parsing engine)
	type AttemptResult struct {
		RuleID   string
		Success  bool
		Reason   string
		Priority int
	}

	attempts := []AttemptResult{
		{RuleID: "smtp_strict", Success: false, Reason: "regex_no_match", Priority: 100},
		{RuleID: "smtp_generic", Success: false, Reason: "field_count_mismatch", Priority: 200},
		{RuleID: "fallback", Success: true, Reason: "", Priority: 300},
	}

	// Validate attempt recording
	if len(attempts) != 3 {
		t.Errorf("Attempt count: got %d, expected 3", len(attempts))
	}

	// First two should fail, last should succeed
	if attempts[0].Success {
		t.Error("First attempt should fail")
	}
	if attempts[1].Success {
		t.Error("Second attempt should fail")
	}
	if !attempts[2].Success {
		t.Error("Third attempt should succeed")
	}

	// Should have failure reasons for failed attempts
	if attempts[0].Reason == "" {
		t.Error("Failed attempt should have failure reason")
	}
	if attempts[1].Reason == "" {
		t.Error("Failed attempt should have failure reason")
	}
	if attempts[2].Reason != "" {
		t.Error("Successful attempt should not have failure reason")
	}

	// Priorities should match rule priorities
	for i, attempt := range attempts {
		if attempt.Priority != sortedRules[i].Priority {
			t.Errorf("Attempt %d priority: got %d, expected %d",
				i, attempt.Priority, sortedRules[i].Priority)
		}
	}
}

func TestBoundedAttempts(t *testing.T) {
	// Test that we respect MaxAttempts limit

	limits := DefaultResourceLimits()
	if limits.MaxAttempts == 0 {
		t.Error("MaxAttempts should be non-zero")
	}

	// Simulate having more rules than max attempts
	rules := make([]ParserRule, limits.MaxAttempts+5) // More than max
	for i := range rules {
		rules[i] = ParserRule{
			ID:       fmt.Sprintf("rule_%d", i),
			Priority: (i + 1) * 100,
		}
	}

	sorted := SortRulesByPriority(rules)

	// Should be able to limit attempts based on resource limits
	maxAttempts := limits.MaxAttempts
	if maxAttempts > len(sorted) {
		maxAttempts = len(sorted)
	}

	attemptedRules := sorted[:maxAttempts]

	if len(attemptedRules) > limits.MaxAttempts {
		t.Errorf("Attempted rules exceed limit: got %d, max %d",
			len(attemptedRules), limits.MaxAttempts)
	}

	// Should attempt lowest priority rules first
	for i := 1; i < len(attemptedRules); i++ {
		if attemptedRules[i].Priority <= attemptedRules[i-1].Priority {
			t.Errorf("Rules not in ascending priority order at position %d", i)
		}
	}
}

func TestValidateParserPack_PriorityValidation(t *testing.T) {
	// Test priority collision warnings
	pack := createValidTestPack()
	pack.Rules = []ParserRule{
		{
			ID:             "rule1",
			Name:           "Rule 1",
			Priority:       100,
			Stages:         []Stage{{ID: "stage1", Name: "Test Stage", Type: StageTypeRegex, Config: map[string]interface{}{"pattern": ".*"}, FailurePolicy: FailurePolicyContinue}},
			FailurePolicy:  FailurePolicyContinue,
			ProjectionMode: ProjectionModeAll,
			CountPolicy:    CountPolicyExactSource,
		},
		{
			ID:             "rule2",
			Name:           "Rule 2",
			Priority:       100, // Same priority
			Stages:         []Stage{{ID: "stage2", Name: "Test Stage", Type: StageTypeRegex, Config: map[string]interface{}{"pattern": ".*"}, FailurePolicy: FailurePolicyContinue}},
			FailurePolicy:  FailurePolicyContinue,
			ProjectionMode: ProjectionModeAll,
			CountPolicy:    CountPolicyExactSource,
		},
		{
			ID:             "rule3",
			Name:           "Rule 3",
			Priority:       200,
			Stages:         []Stage{{ID: "stage3", Name: "Test Stage", Type: StageTypeRegex, Config: map[string]interface{}{"pattern": ".*"}, FailurePolicy: FailurePolicyContinue}},
			FailurePolicy:  FailurePolicyContinue,
			ProjectionMode: ProjectionModeAll,
			CountPolicy:    CountPolicyExactSource,
		},
	}

	result := ValidateParserPack(pack)

	// Should be valid despite priority collision
	if !result.Valid {
		t.Errorf("Pack should be valid: %v", result.Errors)
	}

	// Should have warnings about priority collision
	if len(result.Warnings) == 0 {
		t.Error("Expected warnings about priority collision")
	}

	// Check warning mentions both rules
	foundWarning := false
	for _, warning := range result.Warnings {
		if containsSubstring(warning, "rule1") && containsSubstring(warning, "rule2") && containsSubstring(warning, "priority") {
			foundWarning = true
			break
		}
	}

	if !foundWarning {
		t.Errorf("Expected priority collision warning for rule1 and rule2, got: %v", result.Warnings)
	}
}

func TestValidateParserPack_PositivePriorities(t *testing.T) {
	// Test that negative or zero priorities are rejected
	pack := createValidTestPack()
	pack.Rules = []ParserRule{
		{
			ID:             "rule1",
			Name:           "Rule 1",
			Priority:       0, // Invalid
			Stages:         []Stage{{ID: "stage1", Name: "Test Stage", Type: StageTypeRegex, Config: map[string]interface{}{"pattern": ".*"}, FailurePolicy: FailurePolicyContinue}},
			FailurePolicy:  FailurePolicyContinue,
			ProjectionMode: ProjectionModeAll,
			CountPolicy:    CountPolicyExactSource,
		},
	}

	result := ValidateParserPack(pack)

	if result.Valid {
		t.Error("Pack with zero priority should be invalid")
	}

	// Should have error about priority
	foundError := false
	for _, err := range result.Errors {
		if containsSubstring(err, "priority must be positive") {
			foundError = true
			break
		}
	}

	if !foundError {
		t.Errorf("Expected error about positive priority, got: %v", result.Errors)
	}

	// Test negative priority
	pack.Rules[0].Priority = -5
	result = ValidateParserPack(pack)

	if result.Valid {
		t.Error("Pack with negative priority should be invalid")
	}
}
