// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package errors

import (
	"context"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/logutil"
	"github.com/sirupsen/logrus"
)

// CategorizationTimeout is the hard outer timeout for entire categorization process
const CategorizationTimeout = 5 * time.Second

// CategorizeErrorWithTimeout performs custom error categorization with a 5s timeout.
// Returns nil if categorization fails, times out, or no match is found.
func CategorizeErrorWithTimeout(step *api.StartStepRequest, exitCode int, pipelineEnvs map[string]string) *api.ErrorDetails {
	if !logutil.IsCustomErrorCategorizationEnabled(step.Envs) &&
		!logutil.IsCustomErrorCategorizationEnabled(pipelineEnvs) {
		return nil
	}

	startTime := time.Now()
	ctx, cancel := context.WithTimeout(context.Background(), CategorizationTimeout)
	defer cancel()

	resultCh := make(chan *api.ErrorDetails, 1)

	go func() {
		defer func() {
			if r := recover(); r != nil {
				logrus.WithFields(logrus.Fields{
					"step_id": step.ID,
					"panic":   r,
				}).Warnln("Panic recovered during error categorization")
				resultCh <- nil
			}
		}()
		resultCh <- categorizeError(ctx, step, exitCode, pipelineEnvs)
	}()

	select {
	case result := <-resultCh:
		logrus.WithFields(logrus.Fields{
			"step_id":     step.ID,
			"duration_ms": time.Since(startTime).Milliseconds(),
			"matched":     result != nil,
		}).Infoln("Error categorization completed")
		return result
	case <-ctx.Done():
		logrus.WithFields(logrus.Fields{
			"step_id":     step.ID,
			"duration_ms": time.Since(startTime).Milliseconds(),
		}).Warnln("Error categorization timed out")
		return nil
	}
}

// categorizeError performs the actual error categorization logic
func categorizeError(ctx context.Context, step *api.StartStepRequest, exitCode int, pipelineEnvs map[string]string) *api.ErrorDetails {
	yamlPath, err := ResolveErrorsYAMLPath(step, pipelineEnvs)
	if err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id": step.ID,
			"error":   err.Error(),
		}).Warnln("Error categorization skipped: YAML not found")
		return nil
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
