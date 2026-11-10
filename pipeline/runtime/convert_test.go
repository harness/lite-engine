// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"testing"

	"github.com/harness/lite-engine/api"
)

// TestToStep_EnvsIsCopy asserts toStep does not alias r.Envs. A detached step
// mutates step.Envs in a separate goroutine while the request goroutine still
// reads r.Envs; aliasing the same map caused a concurrent map read+write fatal
// that crashed lite-engine (RetryStartStep failures). toStep must hand the step
// its own map.
func TestToStep_EnvsIsCopy(t *testing.T) {
	r := &api.StartStepRequest{
		ID:   "step-1",
		Envs: map[string]string{"A": "1"},
	}

	step := toStep(r)

	// Mutating the step's map must not touch the request's map.
	step.Envs["B"] = "2"
	if _, ok := r.Envs["B"]; ok {
		t.Fatalf("toStep aliased r.Envs: write to step.Envs leaked into r.Envs")
	}

	// Original values must be preserved in the copy.
	if step.Envs["A"] != "1" {
		t.Fatalf("toStep did not copy existing envs: got step.Envs[A]=%q", step.Envs["A"])
	}
}

// TestToStep_NilEnvsStaysNil preserves prior behavior for steps with no envs.
func TestToStep_NilEnvsStaysNil(t *testing.T) {
	step := toStep(&api.StartStepRequest{ID: "step-1"})
	if step.Envs != nil {
		t.Fatalf("expected nil Envs to stay nil, got %v", step.Envs)
	}
}

// TestConvertStatus_TagsStepTimeout verifies that a step killed by its own
// configured timeout (context.DeadlineExceeded) is tagged with timeout error
// details, so manager-side failure strategies keyed on errors:[timeout] match
// even when lite-engine reports before the orchestration engine's own timeout
// watcher. A manual abort surfaces as context.Canceled and must NOT be tagged.
func TestConvertStatus_TagsStepTimeout(t *testing.T) {
	tests := []struct {
		name       string
		stepErr    error
		wantTagged bool
	}{
		{"deadline exceeded", context.DeadlineExceeded, true},
		{"wrapped deadline exceeded", fmt.Errorf("step execution: %w", context.DeadlineExceeded), true},
		{"manual abort (context canceled)", context.Canceled, false},
		{"generic error", errors.New("boom"), false},
		{"no error", nil, false},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			resp := convertStatus(StepStatus{StepErr: tc.stepErr})
			if !tc.wantTagged {
				if resp.ErrorDetails != nil {
					t.Fatalf("expected no error details, got %+v", resp.ErrorDetails)
				}
				return
			}
			if resp.ErrorDetails == nil {
				t.Fatal("expected timeout error details, got nil")
			}
			if resp.ErrorDetails.FailureType != api.FailureTypeTimeout {
				t.Fatalf("failure type: got %q, want %q", resp.ErrorDetails.FailureType, api.FailureTypeTimeout)
			}
			if resp.ErrorDetails.Message != api.StepTimeoutMessage {
				t.Fatalf("message: got %q, want %q", resp.ErrorDetails.Message, api.StepTimeoutMessage)
			}
			if !resp.ErrorDetails.TimedOut {
				t.Fatal("expected TimedOut to be true")
			}
		})
	}
}


func TestConvertPollResponse_CarriesErrorDetails(t *testing.T) {
	pollResp := &api.PollStepResponse{
		Error:        "context deadline exceeded",
		ErrorDetails: api.TimeoutErrorDetails(),
	}
	resp := convertPollResponse(pollResp, map[string]string{})
	if resp.ErrorDetails == nil {
		t.Fatal("expected error details to be carried through, got nil")
	}
	if resp.ErrorDetails.FailureType != api.FailureTypeTimeout {
		t.Fatalf("failure type: got %q, want %q", resp.ErrorDetails.FailureType, api.FailureTypeTimeout)
	}

	// A plain failure without details must stay without details.
	resp = convertPollResponse(&api.PollStepResponse{Error: "boom"}, map[string]string{})
	if resp.ErrorDetails != nil {
		t.Fatalf("expected nil error details, got %+v", resp.ErrorDetails)
	}
}

// TestToStep_NoRaceBetweenRequestReadAndStepWrite reproduces the production
// crash pattern: the request goroutine reads r.Envs (as isAnnotationsEnabled
// does) while the detached-step goroutine writes the step's env map (as
// executeRunStep does via step.Envs[...]=...). With toStep aliasing r.Envs this
// is a concurrent map read+write; `go test -race` flags it (and the real
// runtime fatal-crashes). With the copy in toStep the two maps are distinct, so
// there is no race. Run: go test -race ./pipeline/runtime/ -run NoRace
func TestToStep_NoRaceBetweenRequestReadAndStepWrite(t *testing.T) {
	r := &api.StartStepRequest{
		ID:   "step-1",
		Envs: map[string]string{annotationsFFEnv: "true", "K": "V"},
	}
	step := toStep(r)

	var wg sync.WaitGroup
	wg.Add(2)

	// writer: mimics executeRunStep mutating the step's env map.
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			step.Envs["DRONE_ENV"] = "/tmp/export.env"
			step.Envs["HARNESS_OUTPUT_FILE"] = "/tmp/out.env"
		}
	}()

	// reader: mimics the request goroutine reading r.Envs.
	go func() {
		defer wg.Done()
		for i := 0; i < 1000; i++ {
			_ = isAnnotationsEnabled(r.Envs)
		}
	}()

	wg.Wait()
}
