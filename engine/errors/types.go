// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

// Field keys for condition matching
const (
	FieldKeyStandardOutput      = "standardOutput"      // Step stdout
	FieldKeyStandardErrorOutput = "standardErrorOutput" // Step stderr
	FieldKeyErrorCode           = "errorCode"           // Exit code
	FieldKeyStepID              = "stepId"              // Step identifier
	FieldKeyStageID             = "stageId"             // Stage identifier
	FieldKeyPipelineID          = "pipelineId"          // Pipeline identifier
)

// Operands for condition matching
const (
	OperandContains     = "contains"     // Case-insensitive substring match
	OperandIs           = "is"           // Exact match (case-sensitive)
	OperandRegex        = "regex"        // Regular expression match
	OperandIsNot        = "isNot"        // Does not match (negation of is, case-sensitive)
	OperandDoesNotMatch = "doesNotMatch" // Does not contain (negation of contains, case-insensitive)
)

// Operators for condition expressions
const (
	OperatorAND = "AND"
	OperatorOR  = "OR"
)

// Action types for rule actions
const (
	ActionTypeSetErrorCategory    = "setErrorCategory"
	ActionTypeSetErrorSubcategory = "setErrorSubcategory"
	ActionTypeSetErrorMessage     = "setErrorMessage"
)

// Evaluation modes
const (
	EvaluationModeFirstMatch = "firstMatch"
)

// Error source constants
const (
	ErrorSourceCustom = "custom" // User-defined rules from errors.yaml
)

// ErrorRules is a wrapper that contains the parsed configuration
// This wrapper structure supports caching and future evaluation logic
type ErrorRules struct {
	Config *ErrorRulesConfig
}

// ErrorRulesConfig represents the root structure of errors.yaml file
// Based on tech spec: https://harness.atlassian.net/wiki/spaces/CI1/pages/23277404336/AI+CI+Tech+spec+Custom+Error+Categorization#Data-Models
type ErrorRulesConfig struct {
	Version    string      `yaml:"version"`
	Settings   Settings    `yaml:"settings,omitempty"` // Optional, defaults to evaluationMode: "firstMatch"
	RuleGroups []RuleGroup `yaml:"ruleGroups"`
}

// Settings contains configuration for error categorization
type Settings struct {
	EvaluationMode string `yaml:"evaluationMode,omitempty"` // Optional, defaults to "firstMatch"
}

// RuleGroup represents a group of error categorization rules
type RuleGroup struct {
	Name                string               `yaml:"name"`
	Enabled             *bool                `yaml:"enabled,omitempty"`   // Optional, defaults to true (nil = true)
	ConditionExpression *ConditionExpression `yaml:"conditionExpression"` // Required (unified format)
	Actions             []Action             `yaml:"actions"`
}

// Condition represents a single condition to match (used internally for validation only)
// This struct is used internally by validateConditionExpression when validating leaf conditions
// RuleGroup only uses ConditionExpression (unified format)
type Condition struct {
	Key     string      `yaml:"key"`     // e.g., FieldKeyStandardOutput, FieldKeyStandardErrorOutput, FieldKeyErrorCode, FieldKeyStepID, FieldKeyStageID, FieldKeyPipelineID
	Operand string      `yaml:"operand"` // e.g., OperandContains, OperandIs, OperandRegex, OperandIsNot, OperandDoesNotMatch
	Value   interface{} `yaml:"value"`   // The value to match against (string, int, etc.)
}

// ConditionExpression represents complex nested conditions with operators
type ConditionExpression struct {
	Operator   string                `yaml:"operator"`   // OperatorAND or OperatorOR
	Conditions []ConditionExpression `yaml:"conditions"` // Nested conditions
	// For leaf conditions (actual condition data)
	Key     string      `yaml:"key,omitempty"`     // e.g., FieldKeyStandardOutput, FieldKeyStandardErrorOutput, FieldKeyErrorCode, FieldKeyStepID, FieldKeyStageID, FieldKeyPipelineID
	Operand string      `yaml:"operand,omitempty"` // e.g., OperandContains, OperandIs, OperandRegex, OperandIsNot, OperandDoesNotMatch
	Value   interface{} `yaml:"value,omitempty"`
}

// Action represents an action to take when a rule matches
type Action struct {
	Type  string `yaml:"type"`  // ActionTypeSetErrorCategory, ActionTypeSetErrorSubcategory, or ActionTypeSetErrorMessage
	Value string `yaml:"value"` // The value to set
}

// ErrorCategorization represents the result of evaluating error rules against step context
type ErrorCategorization struct {
	Category    string // From setErrorCategory action
	Subcategory string // From setErrorSubcategory action
	Message     string // From setErrorMessage action (supports markdown)
	MatchedRule string // Name of the rule group that matched
	Source      string // Source of categorization (e.g., "custom" for user-defined rules)
}
