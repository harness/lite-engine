// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package errors

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/logutil"
	"github.com/sirupsen/logrus"
)

// CategorizationTimeout is the hard outer timeout for entire categorization process
const CategorizationTimeout = 5 * time.Second

type categorizationResult struct {
	result     *EvaluationResult
	durationMs int64
	timedOut   bool
}

// CategorizeErrorWithTimeout performs custom error categorization with a 5s timeout.
// Returns nil only when categorization is disabled or when there is no match and no timeout.
// On timeout, returns ErrorDetails with TimedOut=true and metrics fields populated.
func CategorizeErrorWithTimeout(step *api.StartStepRequest, exitCode int, pipelineEnvs map[string]string) *api.ErrorDetails {
	if !logutil.IsCustomErrorCategorizationEnabled(step.Envs) &&
		!logutil.IsCustomErrorCategorizationEnabled(pipelineEnvs) {
		return nil
	}

	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), CategorizationTimeout)
	defer cancel()

	resultCh := make(chan categorizationResult, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithFields(logrus.Fields{
					"step_id": step.ID,
					"panic":   r,
				}).Warnln("Panic recovered during error categorization")
				resultCh <- categorizationResult{
					durationMs: time.Since(startTime).Milliseconds(),
				}
			}
		}()
		result := categorizeError(ctx, step, exitCode, pipelineEnvs)
		resultCh <- categorizationResult{
			result:     result,
			durationMs: time.Since(startTime).Milliseconds(),
			timedOut:   result != nil && result.TimedOut,
		}
	}()

	var catResult categorizationResult
	select {
	case catResult = <-resultCh:
		logrus.WithFields(logrus.Fields{
			"step_id":     step.ID,
			"duration_ms": catResult.durationMs,
			"matched":     catResult.result != nil && catResult.result.Matched,
			"timed_out":   catResult.timedOut,
		}).Infoln("Error categorization completed")
	case <-ctx.Done():
		catResult = categorizationResult{
			durationMs: time.Since(startTime).Milliseconds(),
			timedOut:   true,
		}
		logrus.WithFields(logrus.Fields{
			"step_id":     step.ID,
			"duration_ms": catResult.durationMs,
		}).Warnln("Error categorization timed out")
	}

	stdoutSize, stderrSize := getLogFileSizes(step.ID)
	return toErrorDetails(catResult, stdoutSize, stderrSize)
}

// categorizeError performs the actual error categorization logic
func categorizeError(ctx context.Context, step *api.StartStepRequest, exitCode int, pipelineEnvs map[string]string) *EvaluationResult {
	yamlPath, err := ResolveErrorsYAMLPath(step, pipelineEnvs)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id": step.ID,
			"error":   err.Error(),
		}).Warnln("Error categorization skipped: YAML not found")
		return nil
	}

	if err := logutil.EnsureCacheDirectory(); err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id": step.ID,
			"error":   err.Error(),
		}).Warnln("Failed to create cache directory")
	}

	cacheDir := logutil.GetCacheDir()
	stdoutPath := logutil.GetStdoutLogFilePath(step.ID)
	stderrPath := logutil.GetStderrLogFilePath(step.ID)

	stageID := getStageID(step)
	pipelineID := getPipelineID(step)

	result, err := InvokeHcliEvaluate(ctx, yamlPath, cacheDir, stdoutPath, stderrPath, exitCode, step.ID, stageID, pipelineID)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id": step.ID,
			"error":   err.Error(),
		}).Warnln("Error categorization failed: hcli error")
		return nil
	}

	return result
}

// getLogFileSizes returns the sizes of the stdout and stderr log files.
func getLogFileSizes(stepID string) (int64, int64) {
	var stdoutSize, stderrSize int64
	if info, err := os.Stat(logutil.GetStdoutLogFilePath(stepID)); err == nil {
		stdoutSize = info.Size()
	}
	if info, err := os.Stat(logutil.GetStderrLogFilePath(stepID)); err == nil {
		stderrSize = info.Size()
	}
	return stdoutSize, stderrSize
}

// toErrorDetails builds an ErrorDetails from a categorizationResult and log file sizes.
// Returns nil if there is no result and no timeout (backward compatible).
func toErrorDetails(catResult categorizationResult, stdoutSize, stderrSize int64) *api.ErrorDetails {
	if catResult.result == nil && !catResult.timedOut {
		return nil
	}

	details := &api.ErrorDetails{
		EvaluationDurationMs: catResult.durationMs,
		StdoutSizeBytes:      stdoutSize,
		StderrSizeBytes:      stderrSize,
		TimedOut:             catResult.timedOut,
	}

	if catResult.result != nil {
		details.FailureType = normalizeEnumName(catResult.result.Category)
		details.FailureSubType = normalizeEnumName(catResult.result.Subcategory)
		details.Message = catResult.result.Message
		details.MatchedRule = catResult.result.MatchedRule
		details.Source = catResult.result.Source
		details.RuleCount = catResult.result.RuleCount
	}

	return details
}

func normalizeEnumName(value string) string {
	if value == "" {
		return ""
	}
	return strings.ToUpper(value)
}

// getStageID extracts stage ID from step request
func getStageID(step *api.StartStepRequest) string {
	if step.TIConfig.StageID != "" {
		return step.TIConfig.StageID
	}
	if val, ok := step.Envs["HARNESS_STAGE_ID"]; ok {
		return val
	}
	return ""
}

// getPipelineID extracts pipeline ID from step request
func getPipelineID(step *api.StartStepRequest) string {
	if step.TIConfig.PipelineID != "" {
		return step.TIConfig.PipelineID
	}
	if val, ok := step.Envs["HARNESS_PIPELINE_ID"]; ok {
		return val
	}
	return ""
}
