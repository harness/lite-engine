// Copyright 2024 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package duallog

import (
	"bytes"
	"encoding/json"
	"io"
	"os"
	"testing"
	"time"
)

func TestNewMetaConfig(t *testing.T) {
	m := NewMetaConfig("acc1", "org1", "proj1", "pipe1", "42", "exec1", "stage1", "step1", "task1")
	if m.AccountID != "acc1" || m.OrgID != "org1" || m.ProjectID != "proj1" ||
		m.PipelineID != "pipe1" || m.RunSequence != "42" || m.PlanExecutionID != "exec1" ||
		m.StageIdentifier != "stage1" || m.StepIdentifier != "step1" || m.TaskID != "task1" {
		t.Errorf("NewMetaConfig did not populate fields correctly: %+v", m)
	}
}

func TestEmitLine(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	meta := &Meta{
		AccountID:       "testAcct",
		OrgID:           "testOrg",
		ProjectID:       "testProj",
		PipelineID:      "testPipe",
		RunSequence:     "99",
		PlanExecutionID: "exec123",
		StageIdentifier: "stg1",
		StepIdentifier:  "stp1",
		TaskID:          "task-abc-DEL",
	}
	ts := time.Unix(1700000000, 500000000)
	EmitLine(meta, "hello world", ts, "LITE_ENGINE_STEP_LOGS")

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	line := buf.String()
	if line == "" {
		t.Fatal("EmitLine produced no output")
	}

	var parsed map[string]interface{}
	if err := json.Unmarshal([]byte(line), &parsed); err != nil {
		t.Fatalf("EmitLine output is not valid JSON: %v\nOutput: %s", err, line)
	}

	if parsed["message"] != "hello world" {
		t.Errorf("expected message 'hello world', got %v", parsed["message"])
	}
	if parsed["level"] != "INFO" {
		t.Errorf("expected level 'INFO', got %v", parsed["level"])
	}
	if parsed["logType"] != "LITE_ENGINE_STEP_LOGS" {
		t.Errorf("expected logType 'LITE_ENGINE_STEP_LOGS', got %v", parsed["logType"])
	}

	abs, ok := parsed["logAbstractions"].(map[string]interface{})
	if !ok {
		t.Fatal("logAbstractions missing or not a map")
	}
	if abs["accountId"] != "testAcct" {
		t.Errorf("expected accountId 'testAcct', got %v", abs["accountId"])
	}
	if abs["stepIdentifier"] != "stp1" {
		t.Errorf("expected stepIdentifier 'stp1', got %v", abs["stepIdentifier"])
	}

	logCtx, ok := parsed["logContext"].(map[string]interface{})
	if !ok {
		t.Fatal("logContext missing or not a map")
	}
	if logCtx["taskId"] != "task-abc-DEL" {
		t.Errorf("expected taskId 'task-abc-DEL', got %v", logCtx["taskId"])
	}

	ts64, ok := parsed["timestamp"].(float64)
	if !ok || ts64 <= 0 {
		t.Errorf("expected positive timestamp, got %v", parsed["timestamp"])
	}
}

func TestEmitLineNoTaskID(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	meta := &Meta{
		AccountID: "testAcct",
	}
	EmitLine(meta, "msg", time.Now(), "TEST")

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	var parsed map[string]interface{}
	if err := json.Unmarshal(buf.Bytes(), &parsed); err != nil {
		t.Fatalf("EmitLine output is not valid JSON: %v", err)
	}
	if _, exists := parsed["logContext"]; exists {
		t.Error("logContext should not be present when taskId is empty")
	}
}

func TestEmitLineNilMetaDoesNotPanic(t *testing.T) {
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	meta := &Meta{}
	EmitLine(meta, "msg", time.Now(), "TEST")

	w.Close()
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, r)
	os.Stdout = oldStdout

	if buf.Len() == 0 {
		t.Error("expected output for empty meta")
	}
}
