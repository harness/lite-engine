// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package errors

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"

	"github.com/harness/lite-engine/api"
	"github.com/sirupsen/logrus"
)

// EvaluationResult represents the JSON output from hcli errors evaluate
type EvaluationResult struct {
	Matched     bool   `json:"matched"`
	Category    string `json:"category,omitempty"`
	Subcategory string `json:"subcategory,omitempty"`
	Message     string `json:"message,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
	Source      string `json:"source,omitempty"`
	Error       string `json:"error,omitempty"`
}

// InvokeHcliEvaluate calls hcli errors evaluate to evaluate rules against log files.
func InvokeHcliEvaluate(ctx context.Context, yamlPath, cacheDir, stdoutPath, stderrPath string, exitCode int, stepID, stageID, pipelineID string) (*api.ErrorDetails, error) {
	cmdArgs := []string{
		"errors", "evaluate",
		"--yaml-path=" + yamlPath,
		"--cache-dir=" + cacheDir,
		"--stdout-path=" + stdoutPath,
		"--stderr-path=" + stderrPath,
		"--exit-code=" + strconv.Itoa(exitCode),
		"--step-id=" + stepID,
		"--stage-id=" + stageID,
		"--pipeline-id=" + pipelineID,
	}

	cmd := exec.CommandContext(ctx, "hcli", cmdArgs...)

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			logrus.WithFields(logrus.Fields{
				"step_id": stepID,
			}).Warnln("hcli evaluate timed out")
			return nil, fmt.Errorf("hcli evaluate timed out: %w", ctx.Err())
		}
		return nil, fmt.Errorf("hcli evaluate failed: %w", err)
	}

	var result EvaluationResult
	if err := json.Unmarshal(output, &result); err != nil {
		return nil, fmt.Errorf("failed to parse hcli evaluate output: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("hcli evaluate error: %s", result.Error)
	}

	if !result.Matched {
		return nil, nil
	}

	return &api.ErrorDetails{
		FailureType:    result.Category,
		FailureSubType: result.Subcategory,
		Message:        result.Message,
		MatchedRule:    result.MatchedRule,
		Source:         result.Source,
	}, nil
}
