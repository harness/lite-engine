// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
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
