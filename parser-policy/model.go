// Package parserpolicy defines the safe declarative ParserPack IR model
// with strict validation and no executable code.
package parserpolicy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Version constants
const (
	CurrentSchemaVersion = "5.0.0"
	MaxPackSize          = 10 * 1024 * 1024 // 10MB
	MaxRules             = 1000
	MaxStages            = 50
	MaxFieldsPerStage    = 500
	MaxRegexLength       = 4096
	MaxInputBytes        = 16 * 1024 * 1024 // 16MB
	MaxExecutionTimeMs   = 30000            // 30 seconds
)

// ParserPack is the top-level container for a versioned parser policy
type ParserPack struct {
	ID              string            `json:"id" validate:"required,min=1,max=64"`
	Version         string            `json:"version" validate:"required,semver"`
	Revision        int               `json:"revision" validate:"min=1"`
	SHA256          string            `json:"sha256" validate:"required,len=64,hexadecimal"`
	SourceType      string            `json:"source_type" validate:"required,oneof=manual imported migrated"`
	SourceHash      string            `json:"source_hash,omitempty" validate:"omitempty,len=64,hexadecimal"`
	Author          string            `json:"author" validate:"required,min=1,max=128"`
	ReviewState     ReviewState       `json:"review_state" validate:"required"`
	CreatedAt       time.Time         `json:"created_at" validate:"required"`
	ActivatedAt     *time.Time        `json:"activated_at,omitempty"`
	Rules           []ParserRule      `json:"rules" validate:"required,min=1,max=1000,dive"`
	ResourceLimits  ResourceLimits    `json:"resource_limits" validate:"required"`
	RegressionCases []RegressionCase  `json:"regression_cases" validate:"dive"`
	Metadata        map[string]string `json:"metadata,omitempty"`
}

type ReviewState string

const (
	ReviewStateDraft     ReviewState = "draft"
	ReviewStatePending   ReviewState = "pending"
	ReviewStateApproved  ReviewState = "approved"
	ReviewStateRejected  ReviewState = "rejected"
	ReviewStateActivated ReviewState = "activated"
	ReviewStateArchived  ReviewState = "archived"
)

// ParserRule represents a single parser with ordered stages
type ParserRule struct {
	ID                string              `json:"id" validate:"required,min=1,max=64"`
	Name              string              `json:"name" validate:"required,min=1,max=128"`
	Priority          int                 `json:"priority" validate:"required,min=1"` // Unbounded positive
	ServiceSelectors  []string            `json:"service_selectors" validate:"dive,min=1,max=64"`
	IncludePredicates []Predicate         `json:"include_predicates,omitempty" validate:"dive"`
	ExcludePredicates []Predicate         `json:"exclude_predicates,omitempty" validate:"dive"`
	Stages            []Stage             `json:"stages" validate:"required,min=1,max=50,dive"`
	FailurePolicy     FailurePolicy       `json:"failure_policy" validate:"required"`
	FieldSchema       map[string]FieldDef `json:"field_schema,omitempty" validate:"dive"`
	ProjectionMode    ProjectionMode      `json:"projection_mode" validate:"required"`
	AllowedFields     []string            `json:"allowed_fields,omitempty" validate:"dive,min=1,max=64"`
	DeniedFields      []string            `json:"denied_fields,omitempty" validate:"dive,min=1,max=64"`
	CountPolicy       CountPolicy         `json:"count_policy" validate:"required"`
}

type Predicate struct {
	Type    string `json:"type" validate:"required,oneof=regex glob exact contains"`
	Field   string `json:"field" validate:"required,min=1,max=64"`
	Pattern string `json:"pattern" validate:"required,min=1,max=1024"`
	Negate  bool   `json:"negate,omitempty"`
}

type FailurePolicy string

const (
	FailurePolicyContinue   FailurePolicy = "continue"
	FailurePolicyRejectRule FailurePolicy = "reject_rule"
	FailurePolicyQuarantine FailurePolicy = "quarantine"
)

type ProjectionMode string

const (
	ProjectionModeAll       ProjectionMode = "all"
	ProjectionModeAllowlist ProjectionMode = "allowlist"
	ProjectionModeDenylist  ProjectionMode = "denylist"
)

type CountPolicy string

const (
	CountPolicyExactSource  CountPolicy = "exact_source_fields"
	CountPolicyAllowDerived CountPolicy = "allow_derived"
	CountPolicyProjection   CountPolicy = "projection"
)

// Stage represents a single parsing stage
type Stage struct {
	ID             string         `json:"id" validate:"required,min=1,max=64"`
	Name           string         `json:"name" validate:"required,min=1,max=128"`
	Type           StageType      `json:"type" validate:"required"`
	Config         interface{}    `json:"config" validate:"required"`
	FailurePolicy  FailurePolicy  `json:"failure_policy" validate:"required"`
	ResourceLimits ResourceLimits `json:"resource_limits,omitempty"`
}

type StageType string

const (
	StageTypeStaticLayout     StageType = "static_layout"
	StageTypeRegex            StageType = "regex"
	StageTypeGrok             StageType = "grok"
	StageTypeMixedKV          StageType = "mixed_kv"
	StageTypeLogfmt           StageType = "logfmt"
	StageTypeJSON             StageType = "json"
	StageTypeTimestamp        StageType = "timestamp"
	StageTypeReplace          StageType = "replace"
	StageTypeRename           StageType = "rename"
	StageTypeTypeConversion   StageType = "type_conversion"
	StageTypeFieldProjection  StageType = "field_projection"
	StageTypeConditionalMatch StageType = "conditional_match"
	StageTypeDrop             StageType = "drop"
	StageTypeOutputMapping    StageType = "output_mapping"
	StageTypeSemanticExtract  StageType = "semantic_extract"
)

// StaticLayoutConfig defines static positional parsing
type StaticLayoutConfig struct {
	Tokens    []TokenDef `json:"tokens" validate:"required,min=1,max=50,dive"`
	Separator string     `json:"separator,omitempty" validate:"max=10"`
}

type TokenDef struct {
	Index     int               `json:"index" validate:"min=0"`
	Name      string            `json:"name" validate:"required,min=1,max=64"`
	Type      TokenType         `json:"type" validate:"required"`
	Config    interface{}       `json:"config,omitempty"`
	Required  bool              `json:"required,omitempty"`
	Optional  bool              `json:"optional,omitempty"`
	Transform []TransformConfig `json:"transform,omitempty" validate:"dive"`
}

type TokenType string

const (
	TokenTypeWhitespace TokenType = "whitespace"
	TokenTypeFixedWidth TokenType = "fixed_width"
	TokenTypeRegex      TokenType = "regex"
	TokenTypeGrok       TokenType = "grok"
	TokenTypeLiteral    TokenType = "literal"
	TokenTypeRestOfLine TokenType = "rest_of_line"
	TokenTypeSplit      TokenType = "split"
)

type TransformConfig struct {
	Type   string            `json:"type" validate:"required,oneof=trim split strip_prefix strip_suffix"`
	Config map[string]string `json:"config,omitempty"`
}

// ResourceLimits defines execution boundaries
type ResourceLimits struct {
	MaxInputBytes       int `json:"max_input_bytes" validate:"min=1,max=16777216"` // 16MB
	MaxFields           int `json:"max_fields" validate:"min=1,max=4096"`
	MaxCaptures         int `json:"max_captures" validate:"min=1,max=1024"`
	MaxRegexLength      int `json:"max_regex_length" validate:"min=1,max=4096"`
	MaxStageCount       int `json:"max_stage_count" validate:"min=1,max=50"`
	MaxAttempts         int `json:"max_attempts" validate:"min=1,max=100"`
	ExecutionDeadlineMs int `json:"execution_deadline_ms" validate:"min=100,max=30000"`
}

// FieldDef defines schema for a field
type FieldDef struct {
	Type                FieldType        `json:"type" validate:"required"`
	Required            bool             `json:"required,omitempty"`
	Enum                []string         `json:"enum,omitempty" validate:"dive,min=1,max=64"`
	Range               *RangeConstraint `json:"range,omitempty"`
	Sensitive           bool             `json:"sensitive,omitempty"`
	PreserveLeadingZero bool             `json:"preserve_leading_zero,omitempty"`
	Aliases             []string         `json:"aliases,omitempty" validate:"dive,min=1,max=64"`
	CollisionPolicy     CollisionPolicy  `json:"collision_policy,omitempty"`
}

type FieldType string

const (
	FieldTypeString FieldType = "string"
	FieldTypeInt    FieldType = "int"
	FieldTypeFloat  FieldType = "float"
	FieldTypeBool   FieldType = "bool"
	FieldTypeTime   FieldType = "time"
	FieldTypeIP     FieldType = "ip"
	FieldTypeURL    FieldType = "url"
	FieldTypeEmail  FieldType = "email"
)

type RangeConstraint struct {
	Min *float64 `json:"min,omitempty"`
	Max *float64 `json:"max,omitempty"`
}

type CollisionPolicy string

const (
	CollisionPolicyError CollisionPolicy = "error"
	CollisionPolicyFirst CollisionPolicy = "first"
	CollisionPolicyLast  CollisionPolicy = "last"
	CollisionPolicyArray CollisionPolicy = "array"
)

// RegressionCase defines exact input/output validation
type RegressionCase struct {
	Name              string                 `json:"name" validate:"required,min=1,max=128"`
	Input             string                 `json:"input" validate:"required,min=1"`
	Service           string                 `json:"service,omitempty" validate:"omitempty,max=64"`
	SourceType        string                 `json:"source_type,omitempty" validate:"omitempty,max=64"`
	ExpectedPayload   map[string]interface{} `json:"expected_payload" validate:"required"`
	ExpectedParser    string                 `json:"expected_parser" validate:"required,min=1,max=128"`
	ExpectedRoute     string                 `json:"expected_route,omitempty" validate:"max=64"`
	ExpectedTelemetry *ExpectedTelemetry     `json:"expected_telemetry,omitempty"`
}

type ExpectedTelemetry struct {
	ParserAttempts []string `json:"parser_attempts"`
	FallbackDepth  int      `json:"fallback_depth"`
	SourceFields   int      `json:"source_fields"`
	OutputFields   int      `json:"output_fields"`
}

// Missing stage types for comprehensive coverage
const (
	StageTypeDelimited StageType = "delimited"
	StageTypeKV        StageType = "key_value"
)

// ValidationResult contains validation errors and warnings
type ValidationResult struct {
	Valid      bool     `json:"valid"`
	Errors     []string `json:"errors,omitempty"`
	Warnings   []string `json:"warnings,omitempty"`
	PackSHA256 string   `json:"pack_sha256,omitempty"`
}

// ValidateParserPack validates a parser pack for safety and correctness
func ValidateParserPack(pack *ParserPack) ValidationResult {
	result := ValidationResult{Valid: true}

	// Basic validation
	if pack == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "pack cannot be nil")
		return result
	}

	// ID validation
	if pack.ID == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "pack ID is required")
	} else if len(pack.ID) > 64 {
		result.Valid = false
		result.Errors = append(result.Errors, "pack ID too long (max 64 chars)")
	} else if !isValidIdentifier(pack.ID) {
		result.Valid = false
		result.Errors = append(result.Errors, "pack ID must be alphanumeric with underscores/hyphens")
	}

	// Version validation (semantic version)
	if pack.Version == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "pack version is required")
	} else if !isValidSemVer(pack.Version) {
		result.Valid = false
		result.Errors = append(result.Errors, "pack version must be valid semantic version (x.y.z)")
	}

	// Revision validation
	if pack.Revision <= 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "pack revision must be positive")
	}

	// Review state validation
	if !isValidReviewState(pack.ReviewState) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("invalid review state: %s", pack.ReviewState))
	}

	// Source type validation
	if !isValidSourceType(pack.SourceType) {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("invalid source type: %s", pack.SourceType))
	}

	// Source hash validation (required if imported/migrated).
	if (pack.SourceType == "imported" || pack.SourceType == "migrated") && pack.SourceHash == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "source hash required for imported/migrated packs")
	}
	if pack.SourceHash != "" && (len(pack.SourceHash) != 64 || !isHexDigest(pack.SourceHash)) {
		result.Valid = false
		result.Errors = append(result.Errors, "source hash must be a 64-character hexadecimal string")
	}
	if pack.CreatedAt.IsZero() {
		result.Valid = false
		result.Errors = append(result.Errors, "created_at is required")
	}

	// Author validation
	if pack.Author == "" {
		result.Valid = false
		result.Errors = append(result.Errors, "pack author is required")
	} else if len(pack.Author) > 128 {
		result.Valid = false
		result.Errors = append(result.Errors, "pack author too long (max 128 chars)")
	}

	// Activation requires an immutable timestamp and at least one exact regression.
	if pack.ReviewState == ReviewStateActivated && pack.ActivatedAt == nil {
		result.Valid = false
		result.Errors = append(result.Errors, "activated packs must have activation timestamp")
	}
	if pack.ReviewState == ReviewStateActivated && len(pack.RegressionCases) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "activated packs must define at least one regression case")
	}
	if pack.ActivatedAt != nil && pack.ReviewState != ReviewStateActivated {
		result.Valid = false
		result.Errors = append(result.Errors, "only activated packs may have activation timestamp")
	}

	// Rules validation
	if len(pack.Rules) == 0 {
		result.Valid = false
		result.Errors = append(result.Errors, "at least one rule is required")
	} else if len(pack.Rules) > MaxRules {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("too many rules (max %d)", MaxRules))
	}

	// Validate rule priorities and uniqueness
	priorities := make(map[int]string)
	ruleIDs := make(map[string]bool)
	for i, rule := range pack.Rules {
		// Check for duplicate rule IDs and the production telemetry identity bound.
		if ruleIDs[rule.ID] {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("duplicate rule ID: %s", rule.ID))
		}
		if len(pack.ID)+1+len(rule.ID) > 100 {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("selected parser identity %s/%s exceeds 100 characters", pack.ID, rule.ID))
		}
		ruleIDs[rule.ID] = true

		// Validate rule
		if err := validateRule(rule, fmt.Sprintf("rule[%d]", i)); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}

		// Track priority collisions for deterministic tie-breaking warning
		if existing, exists := priorities[rule.Priority]; exists {
			result.Warnings = append(result.Warnings,
				fmt.Sprintf("priority collision: rules %s and %s both have priority %d (will be sorted by ID)",
					existing, rule.ID, rule.Priority))
		} else {
			priorities[rule.Priority] = rule.ID
		}
	}

	// Resource limits validation
	if err := validateResourceLimits(pack.ResourceLimits); err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, fmt.Sprintf("resource limits: %v", err))
	}

	// Regression cases validation
	if len(pack.RegressionCases) > 1000 {
		result.Valid = false
		result.Errors = append(result.Errors, "too many regression cases (max 1000)")
	}
	for i, regCase := range pack.RegressionCases {
		if err := validateRegressionCase(regCase, fmt.Sprintf("regression[%d]", i)); err != nil {
			result.Valid = false
			result.Errors = append(result.Errors, err.Error())
		}
	}

	// Verify the canonical hash. Draft programmatic builders may be sealed after
	// construction, but activated packs and strict JSON loads require a hash.
	hash, err := CanonicalParserPackHash(pack)
	if err != nil {
		result.Valid = false
		result.Errors = append(result.Errors, err.Error())
	} else {
		result.PackSHA256 = hash
		if pack.SHA256 == "" {
			if pack.ReviewState == ReviewStateActivated {
				result.Valid = false
				result.Errors = append(result.Errors, "activated parser pack sha256 is required")
			} else {
				result.Warnings = append(result.Warnings, "draft parser pack is not sealed")
			}
		} else if len(pack.SHA256) != 64 || !isHexDigest(pack.SHA256) {
			result.Valid = false
			result.Errors = append(result.Errors, "sha256 must be a 64-character hexadecimal string")
		} else if pack.SHA256 != hash {
			result.Valid = false
			result.Errors = append(result.Errors, fmt.Sprintf("SHA256 mismatch: expected %s, got %s", pack.SHA256, hash))
		}
	}

	return result
}

// validateStage validates individual stage configuration
func validateStage(stage Stage, context string) error {
	if stage.ID == "" {
		return fmt.Errorf("%s: stage ID is required", context)
	}
	if len(stage.ID) > 64 {
		return fmt.Errorf("%s: stage ID too long (max 64 chars)", context)
	}
	if !isValidIdentifier(stage.ID) {
		return fmt.Errorf("%s: stage ID must be alphanumeric with underscores/hyphens", context)
	}

	if stage.Name == "" {
		return fmt.Errorf("%s: stage name is required", context)
	}
	if len(stage.Name) > 128 {
		return fmt.Errorf("%s: stage name too long (max 128 chars)", context)
	}

	if stage.Config == nil {
		return fmt.Errorf("%s: stage config is required", context)
	}

	// Validate type
	switch stage.Type {
	case StageTypeStaticLayout, StageTypeRegex, StageTypeGrok, StageTypeMixedKV, StageTypeLogfmt, StageTypeJSON, StageTypeTimestamp, StageTypeReplace, StageTypeRename, StageTypeTypeConversion, StageTypeFieldProjection, StageTypeConditionalMatch, StageTypeDrop, StageTypeOutputMapping, StageTypeSemanticExtract, StageTypeDelimited, StageTypeKV:
		// Valid types
	default:
		return fmt.Errorf("%s: invalid stage type: %s", context, stage.Type)
	}

	if err := validateStageConfig(stage); err != nil {
		return fmt.Errorf("%s: invalid stage config: %w", context, err)
	}
	if stage.ResourceLimits != (ResourceLimits{}) {
		if err := validateResourceLimits(stage.ResourceLimits); err != nil {
			return fmt.Errorf("%s: invalid stage resource limits: %w", context, err)
		}
	}

	return nil
}

// isValidIdentifier checks if a string is a valid identifier (alphanumeric, underscores, hyphens)
func isValidIdentifier(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-') {
			return false
		}
	}
	return true
}

// isValidSemVer checks if a string is a valid semantic version (x.y.z)
func isValidSemVer(version string) bool {
	parts := strings.Split(version, ".")
	if len(parts) != 3 {
		return false
	}
	for _, part := range parts {
		if part == "" {
			return false
		}
		for _, r := range part {
			if r < '0' || r > '9' {
				return false
			}
		}
	}
	return true
}

// isValidReviewState checks if a review state is valid
func isValidReviewState(state ReviewState) bool {
	switch state {
	case ReviewStateDraft, ReviewStatePending, ReviewStateApproved, ReviewStateRejected, ReviewStateActivated, ReviewStateArchived:
		return true
	default:
		return false
	}
}

// isValidSourceType checks if a source type is valid
func isValidSourceType(sourceType string) bool {
	switch sourceType {
	case "manual", "imported", "migrated":
		return true
	default:
		return false
	}
}

// validateRule validates a single parser rule
func validateRule(rule ParserRule, context string) error {
	// ID validation
	if rule.ID == "" {
		return fmt.Errorf("%s: rule ID is required", context)
	}
	if len(rule.ID) > 64 {
		return fmt.Errorf("%s: rule ID too long (max 64 chars)", context)
	}
	if !isValidIdentifier(rule.ID) {
		return fmt.Errorf("%s: rule ID must be alphanumeric with underscores/hyphens", context)
	}

	// Name validation
	if rule.Name == "" {
		return fmt.Errorf("%s: rule name is required", context)
	}
	if len(rule.Name) > 128 {
		return fmt.Errorf("%s: rule name too long (max 128 chars)", context)
	}

	// Priority validation
	if rule.Priority <= 0 {
		return fmt.Errorf("%s: priority must be positive", context)
	}

	// Service selectors validation
	for i, selector := range rule.ServiceSelectors {
		if selector == "" {
			return fmt.Errorf("%s: service_selectors[%d] cannot be empty", context, i)
		}
		if len(selector) > 64 {
			return fmt.Errorf("%s: service_selectors[%d] too long (max 64 chars)", context, i)
		}
	}

	// Validate predicates
	for i, pred := range rule.IncludePredicates {
		if err := validatePredicate(pred, fmt.Sprintf("%s.include_predicates[%d]", context, i)); err != nil {
			return err
		}
	}
	for i, pred := range rule.ExcludePredicates {
		if err := validatePredicate(pred, fmt.Sprintf("%s.exclude_predicates[%d]", context, i)); err != nil {
			return err
		}
	}

	// Stages validation
	if len(rule.Stages) == 0 {
		return fmt.Errorf("%s: at least one stage is required", context)
	}
	if len(rule.Stages) > MaxStages {
		return fmt.Errorf("%s: too many stages (max %d)", context, MaxStages)
	}

	stageIDs := make(map[string]bool)
	for i, stage := range rule.Stages {
		if stageIDs[stage.ID] {
			return fmt.Errorf("%s: duplicate stage ID: %s", context, stage.ID)
		}
		stageIDs[stage.ID] = true

		if err := validateStage(stage, fmt.Sprintf("%s.stages[%d]", context, i)); err != nil {
			return err
		}
	}

	// Failure policy validation
	if !isValidFailurePolicy(rule.FailurePolicy) {
		return fmt.Errorf("%s: invalid failure policy: %s", context, rule.FailurePolicy)
	}

	// Field schema validation, including cross-field alias collisions.
	aliasOwners := make(map[string]string)
	for fieldName, fieldDef := range rule.FieldSchema {
		if !isValidFieldName(fieldName) {
			return fmt.Errorf("%s.field_schema: invalid field name %q", context, fieldName)
		}
		if err := validateFieldDef(fieldDef, fmt.Sprintf("%s.field_schema[%s]", context, fieldName)); err != nil {
			return err
		}
		for _, alias := range fieldDef.Aliases {
			if alias == fieldName {
				return fmt.Errorf("%s.field_schema[%s]: alias duplicates canonical name", context, fieldName)
			}
			if owner, exists := aliasOwners[alias]; exists && owner != fieldName {
				return fmt.Errorf("%s.field_schema: alias %q belongs to both %s and %s", context, alias, owner, fieldName)
			}
			if _, canonical := rule.FieldSchema[alias]; canonical {
				return fmt.Errorf("%s.field_schema[%s]: alias %q is another canonical field", context, fieldName, alias)
			}
			aliasOwners[alias] = fieldName
		}
	}

	// Projection mode validation
	if !isValidProjectionMode(rule.ProjectionMode) {
		return fmt.Errorf("%s: invalid projection mode: %s", context, rule.ProjectionMode)
	}

	// Projection consistency
	if rule.ProjectionMode == ProjectionModeAllowlist && len(rule.AllowedFields) == 0 {
		return fmt.Errorf("%s: allowlist mode requires allowed_fields", context)
	}
	if rule.ProjectionMode == ProjectionModeDenylist && len(rule.DeniedFields) == 0 {
		return fmt.Errorf("%s: denylist mode requires denied_fields", context)
	}

	// Check for field conflicts
	allowedSet := make(map[string]bool)
	for _, field := range rule.AllowedFields {
		if field == "" {
			return fmt.Errorf("%s: allowed_fields cannot contain empty strings", context)
		}
		if len(field) > 64 {
			return fmt.Errorf("%s: allowed field name too long: %s", context, field)
		}
		allowedSet[field] = true
	}
	deniedSet := make(map[string]bool)
	for _, field := range rule.DeniedFields {
		if field == "" {
			return fmt.Errorf("%s: denied_fields cannot contain empty strings", context)
		}
		if len(field) > 64 {
			return fmt.Errorf("%s: denied field name too long: %s", context, field)
		}
		if allowedSet[field] {
			return fmt.Errorf("%s: field '%s' appears in both allowed and denied lists", context, field)
		}
		deniedSet[field] = true
	}
	if len(rule.FieldSchema) > 0 {
		for field := range allowedSet {
			if _, exists := rule.FieldSchema[field]; !exists {
				return fmt.Errorf("%s: allowed field %q is absent from field_schema", context, field)
			}
		}
		for field, definition := range rule.FieldSchema {
			if !definition.Required {
				continue
			}
			if rule.ProjectionMode == ProjectionModeAllowlist && !allowedSet[field] {
				return fmt.Errorf("%s: required field %q is absent from allowlist", context, field)
			}
			if rule.ProjectionMode == ProjectionModeDenylist && deniedSet[field] {
				return fmt.Errorf("%s: required field %q is denied", context, field)
			}
		}
	}

	// Count policy validation
	if !isValidCountPolicy(rule.CountPolicy) {
		return fmt.Errorf("%s: invalid count policy: %s", context, rule.CountPolicy)
	}
	if rule.CountPolicy == CountPolicyExactSource {
		for _, stage := range rule.Stages {
			if stage.Type == StageTypeSemanticExtract {
				return fmt.Errorf("%s: semantic extraction creates derived fields and requires allow_derived or projection count policy", context)
			}
		}
	}

	return nil
}

// validatePredicate validates a predicate
func validatePredicate(pred Predicate, context string) error {
	if pred.Type == "" {
		return fmt.Errorf("%s: predicate type is required", context)
	}
	switch pred.Type {
	case "regex", "glob", "exact", "contains":
		// Valid types
	default:
		return fmt.Errorf("%s: invalid predicate type: %s", context, pred.Type)
	}

	if pred.Field == "" {
		return fmt.Errorf("%s: predicate field is required", context)
	}
	if len(pred.Field) > 64 {
		return fmt.Errorf("%s: predicate field too long (max 64 chars)", context)
	}

	if pred.Pattern == "" {
		return fmt.Errorf("%s: predicate pattern is required", context)
	}
	if len(pred.Pattern) > 1024 {
		return fmt.Errorf("%s: predicate pattern too long (max 1024 chars)", context)
	}

	// Validate regex patterns
	if pred.Type == "regex" {
		if _, err := regexp.Compile(pred.Pattern); err != nil {
			return fmt.Errorf("%s: invalid regex pattern: %v", context, err)
		}
	}

	return nil
}

// validateFieldDef validates a field definition
func validateFieldDef(fieldDef FieldDef, context string) error {
	if !isValidFieldType(fieldDef.Type) {
		return fmt.Errorf("%s: invalid field type: %s", context, fieldDef.Type)
	}

	// Validate enum values
	if len(fieldDef.Enum) > 100 {
		return fmt.Errorf("%s: too many enum values (max 100)", context)
	}
	for i, enumVal := range fieldDef.Enum {
		if enumVal == "" {
			return fmt.Errorf("%s: enum[%d] cannot be empty", context, i)
		}
		if len(enumVal) > 64 {
			return fmt.Errorf("%s: enum[%d] too long (max 64 chars)", context, i)
		}
	}

	// Validate aliases.
	if len(fieldDef.Aliases) > 10 {
		return fmt.Errorf("%s: too many aliases (max 10)", context)
	}
	aliasSet := make(map[string]bool)
	for i, alias := range fieldDef.Aliases {
		if !isValidFieldName(alias) {
			return fmt.Errorf("%s: aliases[%d] is not a safe field name", context, i)
		}
		if aliasSet[alias] {
			return fmt.Errorf("%s: duplicate alias: %s", context, alias)
		}
		aliasSet[alias] = true
	}
	if fieldDef.Range != nil && fieldDef.Range.Min != nil && fieldDef.Range.Max != nil && *fieldDef.Range.Min > *fieldDef.Range.Max {
		return fmt.Errorf("%s: range min exceeds max", context)
	}
	if fieldDef.Range != nil && fieldDef.Type != FieldTypeInt && fieldDef.Type != FieldTypeFloat {
		return fmt.Errorf("%s: range is valid only for int or float fields", context)
	}
	if fieldDef.PreserveLeadingZero && fieldDef.Type != FieldTypeString {
		return fmt.Errorf("%s: preserve_leading_zero requires string type", context)
	}

	// Validate collision policy
	if fieldDef.CollisionPolicy != "" && !isValidCollisionPolicy(fieldDef.CollisionPolicy) {
		return fmt.Errorf("%s: invalid collision policy: %s", context, fieldDef.CollisionPolicy)
	}

	return nil
}

// validateResourceLimits validates resource limits
func validateResourceLimits(limits ResourceLimits) error {
	if limits.MaxInputBytes < 1 || limits.MaxInputBytes > hardMaxInputBytes {
		return fmt.Errorf("max_input_bytes must be 1-%d", hardMaxInputBytes)
	}
	if limits.MaxFields < 1 || limits.MaxFields > hardMaxFields {
		return fmt.Errorf("max_fields must be 1-%d", hardMaxFields)
	}
	if limits.MaxCaptures < 1 || limits.MaxCaptures > 1024 {
		return fmt.Errorf("max_captures must be 1-1024")
	}
	if limits.MaxRegexLength < 1 || limits.MaxRegexLength > MaxRegexLength {
		return fmt.Errorf("max_regex_length must be 1-%d", MaxRegexLength)
	}
	if limits.MaxStageCount < 1 || limits.MaxStageCount > MaxStages {
		return fmt.Errorf("max_stage_count must be 1-%d", MaxStages)
	}
	if limits.MaxAttempts < 1 || limits.MaxAttempts > 20 {
		return fmt.Errorf("max_attempts must be 1-20")
	}
	if limits.ExecutionDeadlineMs < 100 || limits.ExecutionDeadlineMs > MaxExecutionTimeMs {
		return fmt.Errorf("execution_deadline_ms must be 100-%d", MaxExecutionTimeMs)
	}
	return nil
}

// validateRegressionCase validates a regression test case
func validateRegressionCase(regCase RegressionCase, context string) error {
	if regCase.Name == "" {
		return fmt.Errorf("%s: regression case name is required", context)
	}
	if len(regCase.Name) > 128 {
		return fmt.Errorf("%s: regression case name too long (max 128 chars)", context)
	}

	if regCase.Input == "" {
		return fmt.Errorf("%s: regression case input is required", context)
	}
	if len(regCase.Input) > MaxInputBytes {
		return fmt.Errorf("%s: regression case input too long (max %d bytes)", context, MaxInputBytes)
	}

	if len(regCase.ExpectedPayload) == 0 {
		return fmt.Errorf("%s: regression case expected_payload is required", context)
	}
	if len(regCase.ExpectedPayload) > 1000 {
		return fmt.Errorf("%s: too many expected fields (max 1000)", context)
	}

	if len(regCase.Service) > 64 || len(regCase.SourceType) > 64 {
		return fmt.Errorf("%s: regression service/source_type too long", context)
	}
	if regCase.ExpectedParser == "" {
		return fmt.Errorf("%s: regression case expected_parser is required", context)
	}
	if len(regCase.ExpectedParser) > 128 {
		return fmt.Errorf("%s: regression case expected_parser too long (max 128 chars)", context)
	}

	if len(regCase.ExpectedRoute) > 64 {
		return fmt.Errorf("%s: regression case expected_route too long (max 64 chars)", context)
	}

	return nil
}

// Helper validation functions for enums
func isValidFailurePolicy(policy FailurePolicy) bool {
	switch policy {
	case FailurePolicyContinue, FailurePolicyRejectRule, FailurePolicyQuarantine:
		return true
	default:
		return false
	}
}

func isValidProjectionMode(mode ProjectionMode) bool {
	switch mode {
	case ProjectionModeAll, ProjectionModeAllowlist, ProjectionModeDenylist:
		return true
	default:
		return false
	}
}

func isValidCountPolicy(policy CountPolicy) bool {
	switch policy {
	case CountPolicyExactSource, CountPolicyAllowDerived, CountPolicyProjection:
		return true
	default:
		return false
	}
}

func isValidFieldType(fieldType FieldType) bool {
	switch fieldType {
	case FieldTypeString, FieldTypeInt, FieldTypeFloat, FieldTypeBool, FieldTypeTime, FieldTypeIP, FieldTypeURL, FieldTypeEmail:
		return true
	default:
		return false
	}
}

func isValidCollisionPolicy(policy CollisionPolicy) bool {
	switch policy {
	case CollisionPolicyError, CollisionPolicyFirst, CollisionPolicyLast, CollisionPolicyArray:
		return true
	default:
		return false
	}
}

// Constants for hard limits
const (
	hardMaxInputBytes = 16 * 1024 * 1024
	hardMaxFields     = 4096
)

// SortRulesByPriority sorts rules by ascending priority with deterministic tie-breaking
func SortRulesByPriority(rules []ParserRule) []ParserRule {
	sorted := make([]ParserRule, len(rules))
	copy(sorted, rules)

	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Priority != sorted[j].Priority {
			return sorted[i].Priority < sorted[j].Priority
		}
		// Deterministic tie-breaking by rule ID
		return sorted[i].ID < sorted[j].ID
	})

	return sorted
}

// DefaultResourceLimits returns safe default resource limits
func DefaultResourceLimits() ResourceLimits {
	return ResourceLimits{
		MaxInputBytes:       1024 * 1024, // 1MB
		MaxFields:           256,
		MaxCaptures:         128,
		MaxRegexLength:      1024,
		MaxStageCount:       10,
		MaxAttempts:         5,
		ExecutionDeadlineMs: 5000, // 5 seconds
	}
}

func isValidFieldName(value string) bool {
	if value == "" || len(value) > 128 || strings.HasPrefix(value, "_") || isForbiddenOutputField(value) {
		return false
	}
	for _, r := range value {
		if !((r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '.' || r == '-') {
			return false
		}
	}
	return true
}

func isHexDigest(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, r := range value {
		if !((r >= '0' && r <= '9') || (r >= 'a' && r <= 'f') || (r >= 'A' && r <= 'F')) {
			return false
		}
	}
	return true
}

func isForbiddenOutputField(value string) bool {
	switch strings.ToLower(value) {
	case "raw", "nli", "meta", "source", "envelope", "parser_id", "attempt_count", "ordered_attempts", "parse_telemetry":
		return true
	default:
		return false
	}
}
