// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

// ==============================
// Constants & Guardrails
// ==============================

const (
	// CategorizationTimeout is the hard outer timeout used by step_executor's goroutine wrapper.
	// This is the absolute maximum time error categorization can take before being abandoned.
	CategorizationTimeout = 5 * time.Second

	// EvaluationTimeout is the internal cooperative timeout for rule evaluation (all phases).
	// Set lower than CategorizationTimeout so EvaluateRules can return cleanly
	// (caching partial results) before the outer goroutine is abandoned.
	EvaluationTimeout = 4 * time.Second

	// MaxRuleGroups is the maximum number of rule groups per errors.yaml
	MaxRuleGroups = 100

	// MaxConditionDepth is the maximum nesting depth of condition expressions
	MaxConditionDepth = 20

	// MaxRegexPatternLength prevents ReDoS attacks
	MaxRegexPatternLength = 1000

	// MaxLineSize is the per-line limit (64KB). Lines exceeding this are truncated.
	MaxLineSize = 64 * 1024 // 64KB

	// MaxScannerBufSize is the max buffer the scanner can grow to for very long lines
	MaxScannerBufSize = 512 * 1024 // 512KB

	// MaxLogFileSize is the maximum log file size to scan. Files larger than this
	// are tail-seeked so only the last MaxLogFileSize bytes are evaluated.
	// Errors almost always appear at the end of log output.
	MaxLogFileSize = 25 * 1024 * 1024 // 25MB

	// timeoutCheckInterval controls how often to check context cancellation during file scanning
	timeoutCheckInterval = 10000 // Every 10K lines
)

// ==============================
// Buffer Pool
// ==============================

// scannerBufPool provides reusable 64KB buffers for file scanning via sync.Pool
// Reduces GC pressure when multiple steps fail concurrently
// Memory: ~65KB per active scan, ~130KB per step (2 files), ~13MB for 100 concurrent failures
var scannerBufPool = sync.Pool{
	New: func() interface{} {
		return make([]byte, MaxLineSize)
	},
}

// ==============================
// Log Condition & Result Cache
// ==============================

// logCondition represents a unique condition that requires log file scanning
type logCondition struct {
	operand    string         // OperandContains, OperandIs, etc.
	value      string         // The value to match against
	cacheKey   string         // Pre-computed: {fieldKey}:{operand}:{value}
	regex      *regexp.Regexp // Pre-compiled regex (nil for non-regex operands)
	valueLower string         // Pre-computed lowercase for case-insensitive operands
}

// conditionResultCache stores evaluated condition results
// Thread-safe for parallel stdout/stderr scanning
type conditionResultCache struct {
	mu      sync.Mutex
	results map[string]bool
}

func newConditionResultCache() *conditionResultCache {
	return &conditionResultCache{
		results: make(map[string]bool),
	}
}

func (c *conditionResultCache) set(key string, value bool) {
	c.mu.Lock()
	c.results[key] = value
	c.mu.Unlock()
}

func (c *conditionResultCache) get(key string) (bool, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	v, ok := c.results[key]
	return v, ok
}

// conditionCacheKey generates a unique cache key: {fieldKey}:{operand}:{value}
func conditionCacheKey(fieldKey, operand, value string) string {
	return fieldKey + ":" + operand + ":" + value
}

// ==============================
// Entry Point: EvaluateRules
// ==============================

// EvaluateRules implements the 3-phase evaluation architecture:
//
// Phase 1 — Condition Collection: Walk all expression trees, separate metadata vs log conditions, deduplicate
// Phase 2 — Log File Scanning: Single-pass per file with result caching, parallel when both files needed
// Phase 3 — Expression Evaluation: Evaluate expression trees using cached log results + direct metadata eval
//
// Returns the first matching ErrorCategorization (firstMatch mode) or nil if no rules match.
func EvaluateRules(rules *ErrorRules, stepContext *StepContext) (*ErrorCategorization, error) {
	evalStart := time.Now()
	if rules == nil || rules.Config == nil {
		return nil, fmt.Errorf("rules config is nil")
	}

	// Guardrail: truncate rule groups
	if len(rules.Config.RuleGroups) > MaxRuleGroups {
		logrus.WithFields(logrus.Fields{
			"count": len(rules.Config.RuleGroups),
			"max":   MaxRuleGroups,
		}).Warn("Rule groups exceed maximum, truncating")
		rules.Config.RuleGroups = rules.Config.RuleGroups[:MaxRuleGroups]
	}

	ctx, cancel := context.WithTimeout(context.Background(), EvaluationTimeout)
	defer cancel()

	cache := newConditionResultCache()

	// ===== Phase 1: Condition Collection =====
	phase1Start := time.Now()
	stdoutSeen := make(map[string]bool)
	stderrSeen := make(map[string]bool)
	var stdoutConds, stderrConds []logCondition

	for i := range rules.Config.RuleGroups {
		group := &rules.Config.RuleGroups[i]
		if group.Enabled != nil && !*group.Enabled {
			continue
		}
		collectLogConditions(group.ConditionExpression, &stdoutConds, &stderrConds, stdoutSeen, stderrSeen)
	}

	logrus.WithFields(logrus.Fields{
		"step_id":           stepContext.StepId,
		"stdout_conditions": len(stdoutConds),
		"stderr_conditions": len(stderrConds),
		"duration_ms":       time.Since(phase1Start).Milliseconds(),
	}).Info("Phase 1 complete: conditions collected")

	// ===== Phase 2: Log File Scanning =====
	phase2Start := time.Now()
	if len(stdoutConds) > 0 || len(stderrConds) > 0 {
		if err := scanLogFiles(ctx, stepContext, stdoutConds, stderrConds, cache); err != nil {
			logrus.WithError(err).Warn("Log file scanning encountered errors, continuing with partial results")
		}
		logrus.WithFields(logrus.Fields{
			"step_id":     stepContext.StepId,
			"duration_ms": time.Since(phase2Start).Milliseconds(),
		}).Info("Phase 2 complete: log file scanning")
	} else {
		logrus.Debug("Phase 2 skipped: metadata-only rules, no file I/O needed")
	}

	// ===== Phase 3: Expression Evaluation =====
	phase3Start := time.Now()
	for i := range rules.Config.RuleGroups {
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("evaluation timed out after %v", EvaluationTimeout)
		default:
		}

		group := &rules.Config.RuleGroups[i]
		if group.Enabled != nil && !*group.Enabled {
			continue
		}

		match, err := evaluateExpressionFromCache(ctx, group.ConditionExpression, stepContext, cache, 0)
		if err != nil {
			logrus.WithFields(logrus.Fields{
				"rule_group": group.Name,
				"index":      i,
			}).WithError(err).Warn("Error evaluating rule group, skipping")
			continue
		}

		if match {
			categorization := &ErrorCategorization{
				MatchedRule: group.Name,
				Source:      ErrorSourceCustom,
			}
			for _, action := range group.Actions {
				switch action.Type {
				case ActionTypeSetErrorCategory:
					categorization.Category = action.Value
				case ActionTypeSetErrorSubcategory:
					categorization.Subcategory = action.Value
				case ActionTypeSetErrorMessage:
					categorization.Message = action.Value
				}
			}

			logrus.WithFields(logrus.Fields{
				"step_id":                      stepContext.StepId,
				"rule_group":                   group.Name,
				"category":                     categorization.Category,
				"subcategory":                  categorization.Subcategory,
				"has_message":                  categorization.Message != "",
				"phase3_duration_ms":           time.Since(phase3Start).Milliseconds(),
				"total_evaluation_duration_ms": time.Since(evalStart).Milliseconds(),
			}).Info("Rule matched")
			return categorization, nil
		}
	}

	logrus.WithFields(logrus.Fields{
		"step_id":                      stepContext.StepId,
		"phase3_duration_ms":           time.Since(phase3Start).Milliseconds(),
		"total_evaluation_duration_ms": time.Since(evalStart).Milliseconds(),
	}).Info("No rule groups matched")
	return nil, nil
}

// ==============================
// Phase 1: Condition Collection
// ==============================

// collectLogConditions walks the expression tree and collects unique log conditions
// Metadata conditions (errorCode, stepId, stageId, pipelineId) are skipped — they are evaluated directly in Phase 3
func collectLogConditions(expr *ConditionExpression, stdout, stderr *[]logCondition, stdoutSeen, stderrSeen map[string]bool) {
	if expr == nil {
		return
	}

	// Leaf condition
	if expr.Key != "" {
		if expr.Key != FieldKeyStandardOutput && expr.Key != FieldKeyStandardErrorOutput {
			return // Metadata condition — evaluated directly in Phase 3, no collection needed
		}

		valueStr, ok := expr.Value.(string)
		if !ok {
			return // Invalid value type — will be caught during Phase 3 evaluation
		}

		cKey := conditionCacheKey(expr.Key, expr.Operand, valueStr)

		// Build log condition with pre-computed values
		cond := logCondition{
			operand:  expr.Operand,
			value:    valueStr,
			cacheKey: cKey,
		}

		// Pre-compile regex
		if expr.Operand == OperandRegex {
			if len(valueStr) <= MaxRegexPatternLength {
				re, err := regexp.Compile(valueStr)
				if err == nil {
					cond.regex = re
				} else {
					logrus.WithFields(logrus.Fields{
						"pattern": valueStr,
					}).WithError(err).Warn("Invalid regex pattern during condition collection")
				}
			}
		}

		// Pre-compute lowercase for case-insensitive operands
		if expr.Operand == OperandContains || expr.Operand == OperandDoesNotMatch {
			cond.valueLower = strings.ToLower(valueStr)
		}

		// Deduplicate by cache key
		if expr.Key == FieldKeyStandardOutput {
			if !stdoutSeen[cKey] {
				stdoutSeen[cKey] = true
				*stdout = append(*stdout, cond)
			}
		} else {
			if !stderrSeen[cKey] {
				stderrSeen[cKey] = true
				*stderr = append(*stderr, cond)
			}
		}
		return
	}

	// Node condition — recurse into children
	for i := range expr.Conditions {
		collectLogConditions(&expr.Conditions[i], stdout, stderr, stdoutSeen, stderrSeen)
	}
}

// ==============================
// Phase 2: Log File Scanning
// ==============================

// logFileSize logs the size of a log file before scanning for performance tracking
func logFileSize(path, label, stepId string, needed bool) {
	if !needed {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id": stepId,
			"file":    label,
			"path":    path,
		}).Debug("Log file not found for size check")
		return
	}
	sizeBytes := info.Size()
	sizeMB := float64(sizeBytes) / (1024 * 1024)
	tailOnly := sizeBytes > MaxLogFileSize
	logrus.WithFields(logrus.Fields{
		"step_id":    stepId,
		"file":       label,
		"size_bytes": sizeBytes,
		"size_mb":    fmt.Sprintf("%.2f", sizeMB),
		"tail_only":  tailOnly,
		"path":       path,
	}).Info("Log file size before scanning")
}

// scanLogFiles orchestrates log file scanning with parallel execution when both files are needed
// Total time = max(stdout_time, stderr_time) when parallel
func scanLogFiles(ctx context.Context, stepContext *StepContext, stdoutConds, stderrConds []logCondition, cache *conditionResultCache) error {
	needsStdout := len(stdoutConds) > 0
	needsStderr := len(stderrConds) > 0

	// Log file sizes before scanning for stress test validation
	logFileSize(stepContext.StdoutPath, "stdout", stepContext.StepId, needsStdout)
	logFileSize(stepContext.StderrPath, "stderr", stepContext.StepId, needsStderr)

	// Parallel scanning when both files needed
	if needsStdout && needsStderr {
		logrus.Debug("Phase 2: Scanning both files in parallel")

		var wg sync.WaitGroup
		var stdoutErr, stderrErr error

		wg.Add(2)
		go func() {
			defer wg.Done()
			stdoutErr = scanFileForConditions(ctx, stepContext.StdoutPath, stdoutConds, cache)
		}()
		go func() {
			defer wg.Done()
			stderrErr = scanFileForConditions(ctx, stepContext.StderrPath, stderrConds, cache)
		}()
		wg.Wait()

		if stdoutErr != nil {
			return stdoutErr
		}
		return stderrErr
	}

	// Selective file access: only scan the needed file
	if needsStdout {
		logrus.Debug("Phase 2: Scanning stdout only")
		return scanFileForConditions(ctx, stepContext.StdoutPath, stdoutConds, cache)
	}
	logrus.Debug("Phase 2: Scanning stderr only")
	return scanFileForConditions(ctx, stepContext.StderrPath, stderrConds, cache)
}

// scanFileForConditions performs a single-pass scan of a log file evaluating ALL conditions in one pass
// Results are cached so subsequent rule groups can reuse them without re-scanning
// Uses pooled buffers to minimize allocations (~65KB per active scan)
func scanFileForConditions(ctx context.Context, filePath string, conditions []logCondition, cache *conditionResultCache) error {
	// Handle missing files
	if _, err := os.Stat(filePath); os.IsNotExist(err) {
		logrus.WithField("path", filePath).Debug("Log file does not exist, caching empty results")
		for _, cond := range conditions {
			result, _ := evaluateEmptyContent(cond.operand, cond.value)
			cache.set(cond.cacheKey, result)
		}
		return nil
	}

	file, err := os.Open(filePath)
	if err != nil {
		return fmt.Errorf("failed to open log file %s: %w", filePath, err)
	}
	defer file.Close()

	// Tail-seek: if the file exceeds MaxLogFileSize, skip to the last 25MB.
	// Errors almost always appear at the end of log output.
	info, err := file.Stat()
	if err != nil {
		return fmt.Errorf("failed to stat log file %s: %w", filePath, err)
	}
	tailSeeked := false
	if info.Size() > MaxLogFileSize {
		seekOffset := info.Size() - MaxLogFileSize
		if _, err := file.Seek(seekOffset, io.SeekStart); err != nil {
			return fmt.Errorf("failed to seek log file %s: %w", filePath, err)
		}
		tailSeeked = true
		logrus.WithFields(logrus.Fields{
			"path":         filePath,
			"file_size_mb": fmt.Sprintf("%.2f", float64(info.Size())/(1024*1024)),
			"scan_size_mb": fmt.Sprintf("%.2f", float64(MaxLogFileSize)/(1024*1024)),
			"seek_offset":  seekOffset,
		}).Info("Log file exceeds size limit, scanning tail only")
	}

	// Get pooled buffer (64KB initial)
	buf := scannerBufPool.Get().([]byte)
	defer scannerBufPool.Put(buf)

	scanner := bufio.NewScanner(file)
	scanner.Buffer(buf, MaxScannerBufSize) // 64KB initial, 512KB max

	// After seeking mid-file, the first read lands on a partial line — discard it
	if tailSeeked {
		scanner.Scan()
	}

	// Initialize condition states
	type condState struct {
		cond     logCondition
		resolved bool
		result   bool
	}

	states := make([]condState, len(conditions))
	unresolved := 0

	for i, cond := range conditions {
		states[i] = condState{cond: cond}

		// Handle invalid regex (oversized pattern or compile error)
		if cond.operand == OperandRegex && cond.regex == nil {
			cache.set(cond.cacheKey, false)
			states[i].resolved = true
			continue
		}

		// Set initial result based on operand type
		switch cond.operand {
		case OperandIsNot, OperandDoesNotMatch:
			states[i].result = true // Negative: assume true until a match disproves it
		default:
			states[i].result = false // Positive: assume false until a match proves it
		}

		unresolved++
	}

	// Early exit if all conditions resolved during initialization
	if unresolved == 0 {
		return nil
	}

	linesScanned := 0

	for scanner.Scan() {
		// Periodic timeout check
		if linesScanned > 0 && linesScanned%timeoutCheckInterval == 0 {
			select {
			case <-ctx.Done():
				logrus.WithFields(logrus.Fields{
					"path":          filePath,
					"lines_scanned": linesScanned,
				}).Warn("File scanning timed out, caching partial results")
				for i := range states {
					if !states[i].resolved {
						cache.set(states[i].cond.cacheKey, states[i].result)
					}
				}
				return fmt.Errorf("file scanning timed out for %s after %d lines", filePath, linesScanned)
			default:
			}
		}

		line := scanner.Text()
		linesScanned++

		// Truncate line to 64KB
		if len(line) > MaxLineSize {
			line = line[:MaxLineSize]
		}

		// Lazy lowercase computation (only computed once per line if needed)
		lineLowerComputed := false
		var lineLower string

		for i := range states {
			if states[i].resolved {
				continue
			}

			switch states[i].cond.operand {
			case OperandContains:
				if !lineLowerComputed {
					lineLower = strings.ToLower(line)
					lineLowerComputed = true
				}
				if strings.Contains(lineLower, states[i].cond.valueLower) {
					states[i].result = true
					states[i].resolved = true
					unresolved--
				}

			case OperandIs:
				if line == states[i].cond.value {
					states[i].result = true
					states[i].resolved = true
					unresolved--
				}

			case OperandRegex:
				if states[i].cond.regex.MatchString(line) {
					states[i].result = true
					states[i].resolved = true
					unresolved--
				}

			case OperandIsNot:
				// Negative: if ANY line exactly matches, result is false
				if line == states[i].cond.value {
					states[i].result = false
					states[i].resolved = true
					unresolved--
				}

			case OperandDoesNotMatch:
				// Negative: if ANY line contains the value, result is false
				if !lineLowerComputed {
					lineLower = strings.ToLower(line)
					lineLowerComputed = true
				}
				if strings.Contains(lineLower, states[i].cond.valueLower) {
					states[i].result = false
					states[i].resolved = true
					unresolved--
				}
			}
		}

		// Early termination: all conditions resolved
		if unresolved == 0 {
			logrus.WithFields(logrus.Fields{
				"path":          filePath,
				"lines_scanned": linesScanned,
			}).Debug("All conditions resolved, stopping scan early")
			break
		}
	}

	if err := scanner.Err(); err != nil {
		logrus.WithFields(logrus.Fields{
			"path":          filePath,
			"lines_scanned": linesScanned,
		}).WithError(err).Warn("Error reading log file, caching partial results")
	}

	// Cache all results (resolved and unresolved)
	for i := range states {
		if !states[i].resolved {
			// Unresolved positive operands stay false (no line matched)
			// Unresolved negative operands stay true (no line disproved them)
			states[i].resolved = true
		}
		cache.set(states[i].cond.cacheKey, states[i].result)
	}

	logrus.WithFields(logrus.Fields{
		"path":          filePath,
		"lines_scanned": linesScanned,
		"conditions":    len(conditions),
	}).Debug("File scan complete")

	return nil
}

// ==============================
// Phase 3: Expression Evaluation
// ==============================

// evaluateExpressionFromCache evaluates an expression tree using cached results for log conditions
// and direct evaluation for metadata conditions
func evaluateExpressionFromCache(ctx context.Context, expr *ConditionExpression, stepContext *StepContext, cache *conditionResultCache, depth int) (bool, error) {
	select {
	case <-ctx.Done():
		return false, fmt.Errorf("evaluation timed out at depth %d", depth)
	default:
	}

	if depth > MaxConditionDepth {
		return false, fmt.Errorf("condition depth %d exceeds maximum %d", depth, MaxConditionDepth)
	}

	if expr == nil {
		return false, fmt.Errorf("condition expression is nil")
	}

	// Leaf condition
	if expr.Key != "" {
		return evaluateLeafFromCache(expr.Key, expr.Operand, expr.Value, stepContext, cache)
	}

	// Node condition
	if expr.Operator != "" && len(expr.Conditions) > 0 {
		switch expr.Operator {
		case OperatorAND:
			for i, nested := range expr.Conditions {
				match, err := evaluateExpressionFromCache(ctx, &nested, stepContext, cache, depth+1)
				if err != nil {
					return false, fmt.Errorf("AND condition index %d: %w", i, err)
				}
				if !match {
					return false, nil // Short-circuit
				}
			}
			return true, nil

		case OperatorOR:
			for i, nested := range expr.Conditions {
				match, err := evaluateExpressionFromCache(ctx, &nested, stepContext, cache, depth+1)
				if err != nil {
					logrus.WithFields(logrus.Fields{
						"index": i,
					}).WithError(err).Warn("Error in OR condition, continuing")
					continue
				}
				if match {
					return true, nil // Short-circuit
				}
			}
			return false, nil

		default:
			return false, fmt.Errorf("unknown operator: %s", expr.Operator)
		}
	}

	return false, fmt.Errorf("invalid expression: must be leaf (with key) or node (with operator)")
}

// evaluateLeafFromCache evaluates a single leaf condition
// Log conditions: look up from cache (populated in Phase 2)
// Metadata conditions: evaluate directly (cheap, no I/O)
func evaluateLeafFromCache(key, operand string, value interface{}, stepContext *StepContext, cache *conditionResultCache) (bool, error) {
	// Log conditions → cache lookup
	if key == FieldKeyStandardOutput || key == FieldKeyStandardErrorOutput {
		valueStr, ok := value.(string)
		if !ok {
			return false, fmt.Errorf("rule value must be string for field '%s', got %T", key, value)
		}
		cKey := conditionCacheKey(key, operand, valueStr)
		result, exists := cache.get(cKey)
		if !exists {
			logrus.WithField("cache_key", cKey).Warn("Condition result not in cache, returning false")
			return false, nil
		}
		return result, nil
	}

	// Metadata conditions → direct evaluation (no I/O)
	switch key {
	case FieldKeyErrorCode:
		return evaluateIntCondition(operand, stepContext.ErrorCode, value)
	case FieldKeyStepId:
		return evaluateStringCondition(operand, stepContext.StepId, value)
	case FieldKeyStageId:
		return evaluateStringCondition(operand, stepContext.StageId, value)
	case FieldKeyPipelineId:
		return evaluateStringCondition(operand, stepContext.PipelineId, value)
	default:
		return false, fmt.Errorf("unknown field key: %s", key)
	}
}

// ==============================
// Metadata Evaluation Helpers
// ==============================

// evaluateStringCondition evaluates an in-memory string condition
func evaluateStringCondition(operand, fieldValue string, ruleValue interface{}) (bool, error) {
	ruleValueStr, ok := ruleValue.(string)
	if !ok {
		return false, fmt.Errorf("rule value must be string, got %T", ruleValue)
	}

	switch operand {
	case OperandContains:
		return strings.Contains(strings.ToLower(fieldValue), strings.ToLower(ruleValueStr)), nil
	case OperandIs:
		return fieldValue == ruleValueStr, nil
	case OperandRegex:
		if len(ruleValueStr) > MaxRegexPatternLength {
			return false, fmt.Errorf("regex pattern length %d exceeds maximum %d", len(ruleValueStr), MaxRegexPatternLength)
		}
		re, err := regexp.Compile(ruleValueStr)
		if err != nil {
			return false, fmt.Errorf("invalid regex pattern: %w", err)
		}
		return re.MatchString(fieldValue), nil
	case OperandIsNot:
		return fieldValue != ruleValueStr, nil
	case OperandDoesNotMatch:
		return !strings.Contains(strings.ToLower(fieldValue), strings.ToLower(ruleValueStr)), nil
	default:
		return false, fmt.Errorf("unknown operand: %s", operand)
	}
}

// evaluateIntCondition evaluates an in-memory integer condition
func evaluateIntCondition(operand string, fieldValue int, ruleValue interface{}) (bool, error) {
	if operand == OperandRegex {
		ruleValueStr, ok := ruleValue.(string)
		if !ok {
			return false, fmt.Errorf("regex operand requires string pattern, got %T", ruleValue)
		}
		if len(ruleValueStr) > MaxRegexPatternLength {
			return false, fmt.Errorf("regex pattern length %d exceeds maximum %d", len(ruleValueStr), MaxRegexPatternLength)
		}
		re, err := regexp.Compile(ruleValueStr)
		if err != nil {
			return false, fmt.Errorf("invalid regex pattern: %w", err)
		}
		return re.MatchString(strconv.Itoa(fieldValue)), nil
	}

	var ruleValueInt int
	switch v := ruleValue.(type) {
	case int:
		ruleValueInt = v
	case int64:
		ruleValueInt = int(v)
	case float64:
		ruleValueInt = int(v)
	case string:
		parsed, err := strconv.Atoi(v)
		if err != nil {
			return false, fmt.Errorf("rule value must be numeric for errorCode, got string: %s", v)
		}
		ruleValueInt = parsed
	default:
		return false, fmt.Errorf("rule value must be numeric for errorCode, got %T", ruleValue)
	}

	switch operand {
	case OperandIs:
		return fieldValue == ruleValueInt, nil
	case OperandIsNot:
		return fieldValue != ruleValueInt, nil
	case OperandContains, OperandDoesNotMatch:
		return false, fmt.Errorf("operand '%s' is not supported for errorCode", operand)
	default:
		return false, fmt.Errorf("unknown operand: %s", operand)
	}
}

// evaluateEmptyContent evaluates a condition against empty/missing log content
func evaluateEmptyContent(operand, value string) (bool, error) {
	switch operand {
	case OperandContains:
		return strings.Contains("", strings.ToLower(value)), nil
	case OperandIs:
		return "" == value, nil
	case OperandRegex:
		re, err := regexp.Compile(value)
		if err != nil {
			return false, err
		}
		return re.MatchString(""), nil
	case OperandIsNot:
		return "" != value, nil
	case OperandDoesNotMatch:
		return !strings.Contains("", strings.ToLower(value)), nil
	default:
		return false, fmt.Errorf("unknown operand: %s", operand)
	}
}
