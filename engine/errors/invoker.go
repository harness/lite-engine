// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package errors

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"github.com/sirupsen/logrus"
)

// EvaluationResult mirrors the JSON response from `hcli errors evaluate`.
// Field tags use snake_case to match hcli's JSON output format.
type EvaluationResult struct {
	Matched     bool   `json:"matched"`
	Category    string `json:"category,omitempty"`
	Subcategory string `json:"subcategory,omitempty"`
	Message     string `json:"message,omitempty"`
	MatchedRule string `json:"matched_rule,omitempty"`
	Source      string `json:"source,omitempty"`
	RuleCount   int32  `json:"rule_count,omitempty"`
	TimedOut    bool   `json:"timed_out,omitempty"`
	Error       string `json:"error,omitempty"`
}

// InvokeHcliEvaluate runs `hcli errors evaluate` as a subprocess and parses its JSON output.
// Returns nil result (not an error) when no rule matched and hcli did not time out.
// The ctx deadline is propagated to the subprocess via exec.CommandContext.
func InvokeHcliEvaluate(ctx context.Context, yamlPath, cacheDir, stdoutPath, stderrPath string,
	exitCode int, stepID, stageID, pipelineID string) (*EvaluationResult, error) {
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
	var stderrBuf bytes.Buffer
	cmd.Stderr = &stderrBuf

	output, err := cmd.Output()
	if err != nil {
		if ctx.Err() != nil {
			logrus.WithFields(logrus.Fields{
				"step_id": stepID,
			}).Warnln("hcli evaluate timed out")
			return nil, fmt.Errorf("hcli evaluate timed out: %w", ctx.Err())
		}
		stderrStr := strings.TrimSpace(stderrBuf.String())
		logrus.WithFields(logrus.Fields{
			"step_id": stepID,
			"error":   err,
			"stderr":  stderrStr,
		}).Warnln("hcli evaluate command failed")
		return nil, fmt.Errorf("hcli evaluate failed: %w (stderr: %s)", err, stderrStr)
	}

	var result EvaluationResult
	if err := json.Unmarshal(output, &result); err != nil {
		logrus.WithFields(logrus.Fields{
			"step_id":    stepID,
			"raw_output": strings.TrimSpace(string(output)),
			"stderr":     strings.TrimSpace(stderrBuf.String()),
		}).Warnln("hcli returned non-JSON output")
		return nil, fmt.Errorf("failed to parse hcli evaluate output: %w", err)
	}

	if result.Error != "" {
		return nil, fmt.Errorf("hcli evaluate error: %s", result.Error)
	}

	if !result.Matched && !result.TimedOut {
		return nil, nil
	}

	return &result, nil
}
