// Copyright 2024 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package duallog

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"
)

// Meta holds metadata fields for dual-log JSON payloads.
type Meta struct {
	AccountID       string
	OrgID           string
	ProjectID       string
	PipelineID      string
	RunSequence     string
	PlanExecutionID string
	StageIdentifier string
	StepIdentifier  string
	TaskID          string
}

// NewMetaFromTIConfig constructs a Meta from TI config fields and other sources.
func NewMetaFromTIConfig(accountID, orgID, projectID, pipelineID, buildID, planExecID, stageID, stepID, taskID string) *Meta {
	return &Meta{
		AccountID:       accountID,
		OrgID:           orgID,
		ProjectID:       projectID,
		PipelineID:      pipelineID,
		RunSequence:     buildID,
		PlanExecutionID: planExecID,
		StageIdentifier: stageID,
		StepIdentifier:  stepID,
		TaskID:          taskID,
	}
}

// ExtractStepID extracts the last segment from a logKey of the form
// accountId/orgId/projectId/pipelineId/runSequence/stageId/stepId.
func ExtractStepID(logKey string) string {
	if logKey == "" {
		return ""
	}
	parts := strings.Split(logKey, "/")
	return parts[len(parts)-1]
}

// EmitLine writes a single flat-JSON log line to os.Stdout.
// It uses fmt.Fprintln (NOT logrus) to avoid re-ingestion loops.
func EmitLine(meta *Meta, message string, ts time.Time, logType string) {
	payload := map[string]interface{}{
		"timestamp":  float64(ts.UnixNano()) / 1e9,
		"level":      "INFO",
		"message":    message,
		"logType":    logType,
		"log_source": "streaming",
		"logAbstractions": map[string]string{
			"accountId":       meta.AccountID,
			"orgId":           meta.OrgID,
			"projectId":       meta.ProjectID,
			"pipelineId":      meta.PipelineID,
			"runSequence":     meta.RunSequence,
			"planExecutionId": meta.PlanExecutionID,
			"stageIdentifier": meta.StageIdentifier,
			"stepIdentifier":  meta.StepIdentifier,
		},
	}
	if meta.TaskID != "" {
		payload["logContext"] = map[string]string{
			"taskId": meta.TaskID,
		}
	}
	jsonBytes, err := json.Marshal(payload)
	if err != nil {
		return
	}
	fmt.Fprintln(os.Stdout, string(jsonBytes))
}
