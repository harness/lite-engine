// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"github.com/sirupsen/logrus"
	"gopkg.in/yaml.v3"
)

var (
	// boolTrue is a package-level variable used for default enabled values
	boolTrue = true
)

// ParseErrorsYAML parses and validates the errors.yaml file
func ParseErrorsYAML(yamlPath string) (*ErrorRules, error) {
	// Load YAML file
	yamlContent, err := LoadErrorsYAMLFromPath(yamlPath)
	if err != nil {
		return nil, fmt.Errorf("failed to load errors YAML file: %w", err)
	}

	// Validate YAML structure
	if err := ValidateErrorsYAML(yamlContent); err != nil {
		return nil, fmt.Errorf("failed to validate errors YAML: %w", err)
	}

	// Parse YAML into struct
	var config ErrorRulesConfig
	if err := yaml.Unmarshal(yamlContent, &config); err != nil {
		return nil, fmt.Errorf("failed to parse errors YAML: %w", err)
	}

	// Validate parsed rules
	if err := validateParsedRules(&config); err != nil {
		return nil, fmt.Errorf("failed to validate parsed rules: %w", err)
	}

	// Wrap config in ErrorRules for caching
	rules := &ErrorRules{
		Config: &config,
	}
	return rules, nil
}

// LoadErrorsYAMLFromPath loads the YAML file content from the given path
func LoadErrorsYAMLFromPath(path string) ([]byte, error) {
	// Check if file exists
	if _, err := os.Stat(path); os.IsNotExist(err) {
		return nil, fmt.Errorf("errors YAML file not found at path: %s", path)
	}

	// Read file content
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read errors YAML file: %w", err)
	}

	return content, nil
}

// ValidateErrorsYAML validates the YAML structure
func ValidateErrorsYAML(yamlContent []byte) error {
	if len(yamlContent) == 0 {
		return fmt.Errorf("errors YAML file is empty")
	}

	// Basic YAML syntax validation by attempting to unmarshal into a generic map
	var genericMap map[string]interface{}
	if err := yaml.Unmarshal(yamlContent, &genericMap); err != nil {
		errStr := err.Error()
		// Detect common mistake: YAML starts with list item instead of root map
		if strings.Contains(errStr, "cannot unmarshal !!seq") {
			trimmed := strings.TrimSpace(string(yamlContent))
			if strings.HasPrefix(trimmed, "-") {
				return fmt.Errorf("invalid YAML structure: file starts with a list item ('-') but root must be a map with 'version' and 'ruleGroups' keys. Wrap your ruleGroups in a root structure")
			}
			return fmt.Errorf("invalid YAML structure: root element is a list/array but must be a map/object with 'version' and 'ruleGroups' keys")
		}
		return fmt.Errorf("invalid YAML syntax: %w", err)
	}

	// Check required top-level keys
	if _, exists := genericMap["version"]; !exists {
		return fmt.Errorf("missing required 'version' key at root level of YAML file")
	}
	// settings is optional, defaults to evaluationMode: "firstMatch"
	if _, exists := genericMap["ruleGroups"]; !exists {
		return fmt.Errorf("missing required 'ruleGroups' key at root level of YAML file")
	}

	return nil
}

// validateParsedRules validates the parsed error rules configuration
func validateParsedRules(config *ErrorRulesConfig) error {
	if config == nil {
		return fmt.Errorf("config cannot be nil")
	}

	// Validate version
	if config.Version == "" {
		return fmt.Errorf("version is required")
	}

	// Validate settings (optional, defaults to "firstMatch")
	if config.Settings.EvaluationMode == "" {
		// Default to "firstMatch" if not specified
		config.Settings.EvaluationMode = EvaluationModeFirstMatch
	}
	if config.Settings.EvaluationMode != EvaluationModeFirstMatch {
		return fmt.Errorf("settings.evaluationMode must be '%s', got '%s'", EvaluationModeFirstMatch, config.Settings.EvaluationMode)
	}

	// Validate rule groups
	if len(config.RuleGroups) == 0 {
		return fmt.Errorf("at least one ruleGroup is required")
	}

	// Validate each rule group
	for i := range config.RuleGroups {
		group := &config.RuleGroups[i]
		if group.Name == "" {
			return fmt.Errorf("ruleGroup at index %d has empty name", i)
		}

		// enabled defaults to true if not specified (nil pointer = default to true)
		// If explicitly set to false, keep it false
		if group.Enabled == nil {
			group.Enabled = &boolTrue
		}

		// conditionExpression is required (unified format)
		if group.ConditionExpression == nil {
			return fmt.Errorf("ruleGroup '%s' at index %d is missing required 'conditionExpression' field", group.Name, i)
		}

		// Validate condition expression
		if err := validateConditionExpression(group.ConditionExpression, fmt.Sprintf("ruleGroup '%s'", group.Name)); err != nil {
			return err
		}

		// Validate actions
		if len(group.Actions) == 0 {
			return fmt.Errorf("ruleGroup '%s' at index %d is missing required 'actions' field (must have at least one action)", group.Name, i)
		}

		validActionTypes := map[string]bool{
			ActionTypeSetErrorCategory:    true,
			ActionTypeSetErrorSubcategory: true,
			ActionTypeSetErrorMessage:     true,
		}

		for j, action := range group.Actions {
			if action.Type == "" {
				return fmt.Errorf("ruleGroup '%s' action at index %d has empty type", group.Name, j)
			}
			if !validActionTypes[action.Type] {
				return fmt.Errorf("ruleGroup '%s' action at index %d has invalid type '%s', must be one of: %s, %s, %s",
					group.Name, j, action.Type, ActionTypeSetErrorCategory, ActionTypeSetErrorSubcategory, ActionTypeSetErrorMessage)
			}
			// Value can be empty for some action types, but typically should have a value
		}
	}

	return nil
}

// validateCondition validates a single condition
func validateCondition(condition *Condition, context string) error {
	if condition.Key == "" {
		return fmt.Errorf("%s has empty key", context)
	}

	// Validate field key values
	validFieldKeys := map[string]bool{
		FieldKeyStandardOutput:      true,
		FieldKeyStandardErrorOutput: true,
		FieldKeyErrorCode:           true,
		FieldKeyStepId:              true,
		FieldKeyStageId:             true,
		FieldKeyPipelineId:          true,
	}
	if !validFieldKeys[condition.Key] {
		return fmt.Errorf("%s has invalid key '%s', must be one of: %s, %s, %s, %s, %s, %s", context, condition.Key,
			FieldKeyStandardOutput, FieldKeyStandardErrorOutput, FieldKeyErrorCode, FieldKeyStepId, FieldKeyStageId, FieldKeyPipelineId)
	}

	if condition.Operand == "" {
		return fmt.Errorf("%s has empty operand", context)
	}

	// Validate operand values
	validOperands := map[string]bool{
		OperandContains:     true,
		OperandIs:           true,
		OperandRegex:        true,
		OperandIsNot:        true,
		OperandDoesNotMatch: true,
	}
	if !validOperands[condition.Operand] {
		return fmt.Errorf("%s has invalid operand '%s', must be one of: %s, %s, %s, %s, %s", context, condition.Operand,
			OperandContains, OperandIs, OperandRegex, OperandIsNot, OperandDoesNotMatch)
	}

	// Validate value based on operand type
	if condition.Value == nil {
		return fmt.Errorf("%s has nil value", context)
	}

	// Validate regex pattern if operand is regex
	if condition.Operand == OperandRegex {
		pattern, ok := condition.Value.(string)
		if !ok {
			return fmt.Errorf("%s with operand 'regex' must have string value, got %T", context, condition.Value)
		}
		if pattern == "" {
			return fmt.Errorf("%s with operand 'regex' must have non-empty pattern", context)
		}
		if _, err := regexp.Compile(pattern); err != nil {
			return fmt.Errorf("%s has invalid regex pattern '%s': %w", context, pattern, err)
		}
	}

	// Validate value type based on field key
	if condition.Key == FieldKeyErrorCode {
		// errorCode must be a number (int or float64 from YAML)
		switch condition.Value.(type) {
		case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64, float32, float64:
			// Valid numeric type
		default:
			return fmt.Errorf("%s with key '%s' must have numeric value, got %T", context, condition.Key, condition.Value)
		}
	} else {
		// All other field keys must have string values
		if _, ok := condition.Value.(string); !ok {
			return fmt.Errorf("%s with key '%s' must have string value, got %T", context, condition.Key, condition.Value)
		}
	}

	return nil
}

// validateConditionExpression validates a condition expression (recursive)
func validateConditionExpression(expr *ConditionExpression, context string) error {
	if expr == nil {
		return fmt.Errorf("%s conditionExpression is nil", context)
	}

	// Determine if this is a leaf condition (has key) or a node (has operator and nested conditions)
	isLeaf := expr.Key != ""
	isNode := expr.Operator != "" && len(expr.Conditions) > 0

	// Must be either a leaf or a node, but not both (unless it's a node with key which is invalid)
	if !isLeaf && !isNode {
		return fmt.Errorf("%s conditionExpression must be either a leaf condition (with key/operand/value) or a node (with operator and nested conditions)", context)
	}

	// If it's a leaf condition (has key/operand/value), validate it
	if isLeaf {
		// Leaf conditions should not have operator or nested conditions
		if expr.Operator != "" {
			return fmt.Errorf("%s leaf condition cannot have operator", context)
		}
		if len(expr.Conditions) > 0 {
			return fmt.Errorf("%s leaf condition cannot have nested conditions", context)
		}

		condition := &Condition{
			Key:     expr.Key,
			Operand: expr.Operand,
			Value:   expr.Value,
		}
		return validateCondition(condition, context)
	}

	// If it's a node (has operator and nested conditions), validate them
	if expr.Operator != OperatorAND && expr.Operator != OperatorOR {
		return fmt.Errorf("%s conditionExpression has invalid operator '%s', must be '%s' or '%s'", context, expr.Operator, OperatorAND, OperatorOR)
	}

	if len(expr.Conditions) == 0 {
		return fmt.Errorf("%s conditionExpression with operator '%s' must have at least one nested condition", context, expr.Operator)
	}

	// Validate nested conditions recursively
	for i, nestedExpr := range expr.Conditions {
		if err := validateConditionExpression(&nestedExpr, fmt.Sprintf("%s nested condition at index %d", context, i)); err != nil {
			return err
		}
	}

	return nil
}

// LogParseError logs a parse error with context
func LogParseError(yamlPath string, err error) {
	logrus.WithFields(logrus.Fields{
		"yaml_path": yamlPath,
		"error":     err.Error(),
	}).Warn("Failed to parse errors YAML file")
}
