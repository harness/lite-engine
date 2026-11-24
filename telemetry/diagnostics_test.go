package telemetry

import (
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
)

type fakeClock struct{ nanos atomic.Int64 }

func (c *fakeClock) now() time.Time          { return time.Unix(0, c.nanos.Load()) }
func (c *fakeClock) advance(d time.Duration) { c.nanos.Add(int64(d)) }

func newTestReporter(clock *fakeClock, interval time.Duration, reported *atomic.Int64) *errorReporter {
	return &errorReporter{interval: interval, now: clock.now, report: func(error) { reported.Add(1) }}
}

func TestErrorReporter_ReportsAtMostOncePerInterval(t *testing.T) {
	clock := &fakeClock{}
	clock.advance(time.Hour) // a realistic, non-zero "now"
	var reported atomic.Int64
	r := newTestReporter(clock, time.Minute, &reported)

	r.Handle(errors.New("first"))
	r.Handle(errors.New("suppressed"))
	clock.advance(59 * time.Second)
	r.Handle(errors.New("still suppressed"))
	assert.Equal(t, int64(1), reported.Load())

	clock.advance(2 * time.Second)
	r.Handle(errors.New("next window"))
	assert.Equal(t, int64(2), reported.Load())
}

func TestErrorReporter_IgnoresNilErrors(t *testing.T) {
	var reported atomic.Int64
	r := newTestReporter(&fakeClock{}, time.Minute, &reported)

	r.Handle(nil)

	assert.Equal(t, int64(0), reported.Load())
}

func TestErrorReporter_ASinkPanicIsContained(t *testing.T) {
	r := &errorReporter{interval: time.Minute, now: time.Now, report: func(error) { panic("boom") }}

	assert.NotPanics(t, func() { r.Handle(errors.New("x")) })
}

// Race: the SDK's export goroutines may report simultaneously; exactly one
// of a simultaneous burst may win the window. Run with -race.
func TestErrorReporter_ConcurrentBurstReportsExactlyOnce(t *testing.T) {
	clock := &fakeClock{}
	clock.advance(time.Hour)
	var reported atomic.Int64
	r := newTestReporter(clock, time.Minute, &reported)

	var wg sync.WaitGroup
	start := make(chan struct{})
	for range 32 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			r.Handle(errors.New("burst"))
		}()
	}
	close(start)
	wg.Wait()

	assert.Equal(t, int64(1), reported.Load())
}
