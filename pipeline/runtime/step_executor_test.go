// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"sync"
	"testing"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/errors"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newTestStepExecutor() *StepExecutor {
	return &StepExecutor{
		mu:         sync.Mutex{},
		stepStatus: make(map[string]StepStatus),
		stepLog:    make(map[string]*StepLog),
		stepWaitCh: make(map[string][]chan StepStatus),
	}
}

func TestStartStepWithStatusUpdate_RejectsEmptyID(t *testing.T) {
	e := newTestStepExecutor()
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: ""})
	require.Error(t, err)
	var br *errors.BadRequestError
	assert.ErrorAs(t, err, &br)
}

func TestStartStepWithStatusUpdate_DuplicateRequestIsIgnored(t *testing.T) {
	e := newTestStepExecutor()

	// Simulate a step that's already in flight by seeding stepStatus.
	// This is exactly what the first call sets before spawning its goroutine.
	const stepID = "step-dup-1"
	e.stepStatus[stepID] = StepStatus{Status: Running}

	// A second call with the same ID must be a no-op: return nil without
	// spawning anything and without overwriting the existing status.
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: stepID})
	require.NoError(t, err)

	e.mu.Lock()
	defer e.mu.Unlock()
	got, ok := e.stepStatus[stepID]
	require.True(t, ok)
	assert.Equal(t, Running, got.Status,
		"duplicate request must not overwrite existing status entry")
	assert.Len(t, e.stepStatus, 1, "duplicate request must not add new map entries")
}

func TestStartStepWithStatusUpdate_ConcurrentDuplicatesAreIgnored(t *testing.T) {
	e := newTestStepExecutor()
	const stepID = "step-concurrent"

	// Pre-seed the map to simulate "step already running" — this is what the
	// first caller's idempotency branch does before spawning its goroutine.
	// All concurrent callers below must hit the duplicate branch and bail out
	// without touching the map. (We seed up-front to avoid letting any of
	// these test calls actually spawn a vm_task_executor goroutine, which
	// would then try to run a step with a nil engine.)
	e.stepStatus[stepID] = StepStatus{Status: Running}

	const concurrent = 50
	var wg sync.WaitGroup
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			err := e.StartStepWithStatusUpdate(context.Background(),
				&api.StartStepRequest{ID: stepID})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Exactly one entry, untouched by duplicates.
	e.mu.Lock()
	defer e.mu.Unlock()
	got, ok := e.stepStatus[stepID]
	require.True(t, ok)
	assert.Equal(t, Running, got.Status)
	assert.Len(t, e.stepStatus, 1, "concurrent duplicate calls must not add map entries")
}

func TestStartStepWithStatusUpdate_DifferentIDsCoexist(t *testing.T) {
	e := newTestStepExecutor()

	// Pre-populate two different step IDs as if both are running.
	e.stepStatus["step-a"] = StepStatus{Status: Running}
	e.stepStatus["step-b"] = StepStatus{Status: Running}

	// A duplicate of one ID must not affect the other.
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: "step-a"})
	require.NoError(t, err)

	e.mu.Lock()
	defer e.mu.Unlock()
	assert.Len(t, e.stepStatus, 2)
	assert.Equal(t, Running, e.stepStatus["step-a"].Status)
	assert.Equal(t, Running, e.stepStatus["step-b"].Status)
}
