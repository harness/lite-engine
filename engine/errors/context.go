// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package errors

import (
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/logutil"
	"github.com/sirupsen/logrus"
)

// Environment variable keys for stage and pipeline identifiers
const (
	HarnessStageIDEnv    = "HARNESS_STAGE_ID"
	HarnessPipelineIDEnv = "HARNESS_PIPELINE_ID"
)

// StepContext contains the execution context for error rule evaluation
// Contains only metadata identifiers (exitCode, stepID, stageID, pipelineID)
// Log file paths are stored for line-by-line streaming during evaluation — content is NOT loaded into memory
type StepContext struct {
	StdoutPath string // Path to stdout log file (streamed line-by-line during evaluation)
	StderrPath string // Path to stderr log file (streamed line-by-line during evaluation)
	ErrorCode  int    // Exit code from step execution
	StepID     string // Step identifier (from step request)
	StageID    string // Stage identifier (from environment)
	PipelineID string // Pipeline identifier (from environment)
}

// BuildStepContext builds step execution context from available data sources
// exitCode is the exit code from step execution.
// Defaults to 1 if exitCode is 0 (which means step didn't fail — backwards compatibility).
// Resolves log file paths but does NOT read content — files are streamed line-by-line during evaluation
func BuildStepContext(step *api.StartStepRequest, exitCode int) (*StepContext, error) {
	ctx := &StepContext{
		ErrorCode: 1, // Default exit code for failures
	}

	// Get step identifier
	ctx.StepID = step.ID

	// Get stage identifier from step environment variables
	if stageID, ok := step.Envs[HarnessStageIDEnv]; ok && stageID != "" {
		ctx.StageID = stageID
	} else {
		logrus.WithField("step_id", step.ID).Debug("Could not get stage ID from step envs, using empty string")
	}

	// Get pipeline identifier from step environment variables
	if pipelineID, ok := step.Envs[HarnessPipelineIDEnv]; ok && pipelineID != "" {
		ctx.PipelineID = pipelineID
	} else {
		logrus.WithField("step_id", step.ID).Debug("Could not get pipeline ID from step envs, using empty string")
	}

	// Use exit code from step execution
	// If 0, it means step succeeded — keep default of 1 for error categorization context
	if exitCode != 0 {
		ctx.ErrorCode = exitCode
	}

	// Resolve log file paths using logutil (NOT reading content — will be streamed during evaluation)
	ctx.StdoutPath = logutil.GetStdoutLogFilePath(step.ID)
	ctx.StderrPath = logutil.GetStderrLogFilePath(step.ID)

	logrus.WithFields(logrus.Fields{
		"step_id":     ctx.StepID,
		"stdout_path": ctx.StdoutPath,
		"stderr_path": ctx.StderrPath,
		"error_code":  ctx.ErrorCode,
		"stage_id":    ctx.StageID,
		"pipeline_id": ctx.PipelineID,
	}).Debug("Built step context")

	return ctx, nil
}
