package runtime

import (
	"context"
	"errors"
	"io"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	drone "github.com/drone/runner-go/pipeline/runtime"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/telemetry"
)

type stepSpy struct {
	started  atomic.Int64
	finished atomic.Int64
}

func (*stepSpy) ObserveCall(telemetry.Subsystem, telemetry.Operation, time.Duration, error) {}
func (s *stepSpy) StepStarted()                                                             { s.started.Add(1) }
func (s *stepSpy) StepFinished()                                                            { s.finished.Add(1) }

func runWith(f RunFunc) error {
	_, err := f(context.Background(), &spec.Step{}, io.Discard, false, false)
	return err
}

func TestTrackedRunFunc_StepIsRunningWhileItExecutesAndFinishedAfter(t *testing.T) {
	spy := &stepSpy{}
	var startedDuring, finishedDuring int64

	err := runWith(trackedRunFunc(func(context.Context, *spec.Step, io.Writer, bool, bool) (*drone.State, error) {
		startedDuring, finishedDuring = spy.started.Load(), spy.finished.Load()
		return &drone.State{}, nil
	}, spy))

	require.NoError(t, err)
	assert.Equal(t, int64(1), startedDuring)
	assert.Equal(t, int64(0), finishedDuring)
	assert.Equal(t, int64(1), spy.finished.Load())
}

func TestTrackedRunFunc_ResultAndErrorPassThroughUnchanged(t *testing.T) {
	want := errors.New("step failed")
	state := &drone.State{ExitCode: 7}
	spy := &stepSpy{}

	gotState, gotErr := trackedRunFunc(func(context.Context, *spec.Step, io.Writer, bool, bool) (*drone.State, error) {
		return state, want
	}, spy)(context.Background(), &spec.Step{}, io.Discard, false, false)

	assert.Same(t, state, gotState)
	assert.Same(t, want, gotErr)
	assert.Equal(t, int64(1), spy.finished.Load(), "a failed step still finishes")
}

func TestTrackedRunFunc_APanickingStepIsStillMarkedFinishedAndThePanicPropagates(t *testing.T) {
	spy := &stepSpy{}
	run := trackedRunFunc(func(context.Context, *spec.Step, io.Writer, bool, bool) (*drone.State, error) {
		panic("step exploded")
	}, spy)

	assert.PanicsWithValue(t, "step exploded", func() { _ = runWith(run) }, "tracking must not swallow the step's own panic")

	assert.Equal(t, int64(1), spy.finished.Load(), "otherwise the running count would leak upward forever")
}

func TestTrackedRunFunc_NilRecorderLeavesTheStepAlone(t *testing.T) {
	ran := false

	require.NoError(t, runWith(trackedRunFunc(func(context.Context, *spec.Step, io.Writer, bool, bool) (*drone.State, error) {
		ran = true
		return &drone.State{}, nil
	}, nil)))

	assert.True(t, ran)
}

// Race: a stage runs many steps in parallel through one decorated RunFunc.
func TestTrackedRunFunc_ParallelStepsBalanceOut(t *testing.T) {
	spy := &stepSpy{}
	run := trackedRunFunc(func(context.Context, *spec.Step, io.Writer, bool, bool) (*drone.State, error) {
		return &drone.State{}, nil
	}, spy)

	const steps = 200
	var wg sync.WaitGroup
	for range steps {
		wg.Add(1)
		go func() { defer wg.Done(); _ = runWith(run) }()
	}
	wg.Wait()

	assert.Equal(t, int64(steps), spy.started.Load())
	assert.Equal(t, int64(steps), spy.finished.Load())
}
