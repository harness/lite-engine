package osstats

import (
	"context"
	"sync"
	"testing"
	"time"
)

// TestStatsCollector_StopIdempotent verifies that Stop() can be safely invoked
// more than once. The previous implementation called close(s.doneCh) directly
// and panicked on a second call (close-of-closed channel); the sync.Once guard
// added in collector.go must serialize all Stop calls.
func TestStatsCollector_StopIdempotent(t *testing.T) {
	c := New(context.Background(), 100*time.Millisecond, false)

	// Calling Stop twice serially must not panic.
	c.Stop()
	c.Stop()
}

// TestStatsCollector_StopConcurrent runs many parallel Stop() callers; if the
// sync.Once guard is missing, the race detector or the runtime will flag a
// close-of-closed-channel panic in at least one goroutine.
func TestStatsCollector_StopConcurrent(t *testing.T) {
	c := New(context.Background(), 100*time.Millisecond, false)

	const N = 32
	var wg sync.WaitGroup
	wg.Add(N)
	for i := 0; i < N; i++ {
		go func() {
			defer wg.Done()
			c.Stop()
		}()
	}
	wg.Wait()
}
