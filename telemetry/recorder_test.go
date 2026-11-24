package telemetry

import (
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// observedCall is one ObserveCall invocation captured by fakeRecorder.
type observedCall struct {
	sub Subsystem
	op  Operation
	d   time.Duration
	err error
}

// fakeRecorder is a concurrency-safe Recorder that remembers what it was told.
type fakeRecorder struct {
	mu       sync.Mutex
	calls    []observedCall
	started  int
	finished int
}

func (f *fakeRecorder) ObserveCall(sub Subsystem, op Operation, d time.Duration, err error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, observedCall{sub: sub, op: op, d: d, err: err})
}

func (f *fakeRecorder) StepStarted() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.started++
}

func (f *fakeRecorder) StepFinished() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.finished++
}

func (f *fakeRecorder) snapshot() (calls []observedCall, started, finished int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]observedCall(nil), f.calls...), f.started, f.finished
}

// panickingRecorder simulates a buggy/hostile telemetry backend.
type panickingRecorder struct{}

func (panickingRecorder) ObserveCall(Subsystem, Operation, time.Duration, error) { panic("boom") }
func (panickingRecorder) StepStarted()                                           { panic("boom") }
func (panickingRecorder) StepFinished()                                          { panic("boom") }

func TestObserve_ReturnsCallbackErrorUnchangedAndRecordsIt(t *testing.T) {
	rec := &fakeRecorder{}
	want := errors.New("log service down")

	got := Observe(rec, SubsystemLogService, OpLogWrite, func() error {
		time.Sleep(5 * time.Millisecond)
		return want
	})

	require.Same(t, want, got, "the caller must receive the exact error from the wrapped call")
	calls, _, _ := rec.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, SubsystemLogService, calls[0].sub)
	assert.Equal(t, OpLogWrite, calls[0].op)
	assert.Same(t, want, calls[0].err)
	assert.GreaterOrEqual(t, calls[0].d, 5*time.Millisecond)
}

func TestObserve_RecordsSuccessWithNilError(t *testing.T) {
	rec := &fakeRecorder{}

	require.NoError(t, Observe(rec, SubsystemTIService, OpTIReport, func() error { return nil }))

	calls, _, _ := rec.snapshot()
	require.Len(t, calls, 1)
	assert.NoError(t, calls[0].err)
}

func TestObserve_RecorderPanicNeverReachesTheCaller(t *testing.T) {
	want := errors.New("real failure")

	var got error
	require.NotPanics(t, func() {
		got = Observe(panickingRecorder{}, SubsystemLogService, OpLogOpen, func() error { return want })
	})

	assert.Same(t, want, got, "a panicking recorder must not change the wrapped call's result")
}

func TestObserve_NilRecorderStillRunsTheCall(t *testing.T) {
	ran := false

	err := Observe(nil, SubsystemLogService, OpLogClose, func() error { ran = true; return nil })

	require.NoError(t, err)
	assert.True(t, ran)
}

func TestNop_AcceptsEverything(t *testing.T) {
	rec := Nop()

	require.NotPanics(t, func() {
		rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Second, errors.New("x"))
		rec.StepStarted()
		rec.StepFinished()
	})
}

func TestDelegatingRecorder_IsNopUntilATargetIsSet(t *testing.T) {
	d := newDelegatingRecorder()

	require.NotPanics(t, func() { d.StepStarted() })
}

func TestDelegatingRecorder_ForwardsToCurrentTargetAndFallsBackToNop(t *testing.T) {
	d := newDelegatingRecorder()
	first, second := &fakeRecorder{}, &fakeRecorder{}

	d.set(first)
	d.StepStarted()
	d.set(second)
	d.StepStarted()
	d.StepFinished()
	d.set(nil) // clearing must revert to nop, not nil-deref
	d.StepStarted()

	_, firstStarted, _ := first.snapshot()
	_, secondStarted, secondFinished := second.snapshot()
	assert.Equal(t, 1, firstStarted)
	assert.Equal(t, 1, secondStarted)
	assert.Equal(t, 1, secondFinished)
}

// Race: recorders are hammered from many goroutines while the target is swapped
// (Start/Shutdown happening concurrently with live traffic). Run with -race.
func TestDelegatingRecorder_ConcurrentUseAndSwapIsRaceFree(t *testing.T) {
	d := newDelegatingRecorder()
	a, b := &fakeRecorder{}, &fakeRecorder{}

	const workers, iterations = 8, 500
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				d.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
				d.StepStarted()
				d.StepFinished()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for i := range iterations {
			if i%2 == 0 {
				d.set(a)
			} else {
				d.set(b)
			}
		}
	}()
	wg.Wait()

	aCalls, _, _ := a.snapshot()
	bCalls, _, _ := b.snapshot()
	assert.LessOrEqual(t, len(aCalls)+len(bCalls), workers*iterations)
}

func TestTrackStep_RecordsStartNowAndFinishWhenDoneIsCalled(t *testing.T) {
	rec := &fakeRecorder{}

	done := TrackStep(rec)
	_, started, finished := rec.snapshot()
	assert.Equal(t, 1, started)
	assert.Equal(t, 0, finished, "a step that has not returned is still running")

	done()
	_, started, finished = rec.snapshot()
	assert.Equal(t, 1, started)
	assert.Equal(t, 1, finished)
}

func TestTrackStep_RecorderPanicsNeverReachTheStep(t *testing.T) {
	require.NotPanics(t, func() {
		done := TrackStep(panickingRecorder{})
		done()
	})
}

func TestTrackStep_NilRecorderIsHarmless(t *testing.T) {
	require.NotPanics(t, func() { TrackStep(nil)() })
}
