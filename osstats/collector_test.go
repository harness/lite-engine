// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package osstats

import (
	"context"
	"math"
	"sync"
	"testing"
	"time"
)

// TestStatsCollector_ConcurrentReadAndCollect runs the collector while
// multiple goroutines repeatedly read Stats(). The background collectStats
// goroutine mutates s.stats fields (MemGraph.Points, MaxMemUsagePct, etc.)
// without synchronization, while Stats() returns the live pointer — so any
// reader that touches the returned struct's fields can race with update().
//
// Run with `go test -race -run TestStatsCollector_ -count=3`.
func TestStatsCollector_ConcurrentReadAndCollect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// cpu.Percent uses a 1s sample window inside get(); keep the interval
	// small but accept that the first sample takes ~1s.
	c := New(ctx, 50*time.Millisecond, false)
	c.Start()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Concurrent readers — touch every field that update() mutates.
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				st := c.Stats()
				_ = st.MaxMemUsagePct
				_ = st.MaxCPUUsagePct
				_ = st.AvgMemUsagePct
				_ = st.AvgCPUUsagePct
				_ = st.TotalMemMB
				_ = st.CPUCores
				if st.MemGraph != nil {
					_ = len(st.MemGraph.Points)
				}
				if st.CPUGraph != nil {
					_ = len(st.CPUGraph.Points)
				}
			}
		}()
	}

	// Let the collector run through at least a couple of update cycles.
	time.Sleep(2500 * time.Millisecond)

	close(stop)
	wg.Wait()
	c.Stop()
}

// TestStatsCollector_AggregateRacesCollect calls Aggregate() concurrently
// with the running collector. Aggregate() reads memPctSum/cpuPctSum and
// rewrites MemGraph.Points/CPUGraph.Points; the collector goroutine is
// concurrently appending to those same slices via update(). Without
// synchronization this is a clear race.
func TestStatsCollector_AggregateRacesCollect(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := New(ctx, 50*time.Millisecond, false)
	c.Start()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			case <-time.After(20 * time.Millisecond):
				c.Aggregate()
			}
		}
	}()

	// Run long enough that a real update() likely overlaps an Aggregate().
	time.Sleep(2500 * time.Millisecond)

	close(stop)
	wg.Wait()
	c.Stop()
}

// TestStatsCollector_StopWhileReading covers the teardown race: Stop() closes
// doneCh, the collector goroutine returns, but readers may still be in
// flight against the same s.stats pointer.
func TestStatsCollector_StopWhileReading(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := New(ctx, 50*time.Millisecond, false)
	c.Start()

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				_ = c.Stats()
			}
		}()
	}

	// Let one update happen, then stop while readers are still going.
	time.Sleep(1500 * time.Millisecond)
	c.Stop()
	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()
}

// TestPercentileNearestRank_Empty checks that an empty slice returns 0.
func TestPercentileNearestRank_Empty(t *testing.T) {
	if got := percentileNearestRank(nil, 0.90); got != 0 {
		t.Fatalf("expected 0 for nil slice, got %f", got)
	}
	if got := percentileNearestRank([]float64{}, 0.50); got != 0 {
		t.Fatalf("expected 0 for empty slice, got %f", got)
	}
}

// TestPercentileNearestRank_SingleValue checks that a single-element slice
// always returns that element for any percentile.
func TestPercentileNearestRank_SingleValue(t *testing.T) {
	vals := []float64{42.5}
	for _, p := range []float64{0.50, 0.90, 0.95, 0.99} {
		got := percentileNearestRank(vals, p)
		if got != 42.5 {
			t.Fatalf("p=%.2f: expected 42.5, got %f", p, got)
		}
	}
}

// TestPercentileNearestRank_KnownValues tests with a 10-element sorted input
// so the expected indices are easy to verify by hand.
//
// Sorted input: [10, 20, 30, 40, 50, 60, 70, 80, 90, 100]
//
//	P50: ceil(0.50*10)-1 = 4  -> 50
//	P90: ceil(0.90*10)-1 = 8  -> 90
//	P95: ceil(0.95*10)-1 = 9  -> 100
//	P99: ceil(0.99*10)-1 = 9  -> 100
func TestPercentileNearestRank_KnownValues(t *testing.T) {
	// Intentionally unsorted to verify the function sorts internally
	vals := []float64{50, 10, 90, 30, 70, 20, 60, 100, 40, 80}

	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 50},
		{0.90, 90},
		{0.95, 100},
		{0.99, 100},
	}
	for _, tc := range cases {
		got := percentileNearestRank(vals, tc.p)
		if got != tc.want {
			t.Errorf("p=%.2f: expected %f, got %f", tc.p, tc.want, got)
		}
	}
}

// TestPercentileNearestRank_DoesNotMutateInput checks that the original
// slice is not sorted in place.
func TestPercentileNearestRank_DoesNotMutateInput(t *testing.T) {
	vals := []float64{5, 3, 1, 4, 2}
	orig := make([]float64, len(vals))
	copy(orig, vals)

	_ = percentileNearestRank(vals, 0.90)

	for i := range vals {
		if vals[i] != orig[i] {
			t.Fatalf("input was mutated at index %d: expected %f, got %f", i, orig[i], vals[i])
		}
	}
}

// TestAggregate_PercentilesComputed feeds known samples into the collector
// and verifies that Aggregate() produces correct percentile values.
func TestAggregate_PercentilesComputed(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := New(ctx, time.Hour, false) // long interval so collectStats won't fire

	// Manually feed 10 samples (bypassing the goroutine)
	for i := 1; i <= 10; i++ {
		c.update(&osStat{
			CPUPct:     float64(i * 10), // 10, 20, 30, ..., 100
			MemPct:     float64(i * 5),  // 5, 10, 15, ..., 50
			MemTotalMB: 8192,
			CPUTotal:   4,
		})
	}

	c.Aggregate()
	stats := c.Stats()

	// CPU: sorted = [10,20,30,40,50,60,70,80,90,100]
	assertClose(t, "P50CPU", stats.P50CPUUsagePct, 50)
	assertClose(t, "P90CPU", stats.P90CPUUsagePct, 90)
	assertClose(t, "P95CPU", stats.P95CPUUsagePct, 100)
	assertClose(t, "P99CPU", stats.P99CPUUsagePct, 100)

	// Memory: sorted = [5,10,15,20,25,30,35,40,45,50]
	assertClose(t, "P50Mem", stats.P50MemUsagePct, 25)
	assertClose(t, "P90Mem", stats.P90MemUsagePct, 45)
	assertClose(t, "P95Mem", stats.P95MemUsagePct, 50)
	assertClose(t, "P99Mem", stats.P99MemUsagePct, 50)

	// Existing fields should still work
	assertClose(t, "AvgCPU", stats.AvgCPUUsagePct, 55)  // (10+20+...+100)/10
	assertClose(t, "AvgMem", stats.AvgMemUsagePct, 27.5) // (5+10+...+50)/10
	assertClose(t, "MaxCPU", stats.MaxCPUUsagePct, 100)
	assertClose(t, "MaxMem", stats.MaxMemUsagePct, 50)

	if stats.CPUCores != 4 {
		t.Errorf("CPUCores: expected 4, got %d", stats.CPUCores)
	}
	if stats.TotalMemMB != 8192 {
		t.Errorf("TotalMemMB: expected 8192, got %f", stats.TotalMemMB)
	}
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: expected %.2f, got %.2f", name, want, got)
	}
}

// TestStatsCollector_ConcurrentReadWithPercentiles is the same as the
// original concurrency test but also reads the new percentile fields,
// making sure they don't race with update() or Aggregate().
func TestStatsCollector_ConcurrentReadWithPercentiles(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	c := New(ctx, 50*time.Millisecond, false)
	c.Start()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
				}
				st := c.Stats()
				_ = st.MaxMemUsagePct
				_ = st.MaxCPUUsagePct
				_ = st.P50CPUUsagePct
				_ = st.P90CPUUsagePct
				_ = st.P95CPUUsagePct
				_ = st.P99CPUUsagePct
				_ = st.P50MemUsagePct
				_ = st.P90MemUsagePct
				_ = st.P95MemUsagePct
				_ = st.P99MemUsagePct
				_ = st.DiskTotalReadMB
				_ = st.DiskTotalWriteMB
			}
		}()
	}

	time.Sleep(2500 * time.Millisecond)

	close(stop)
	wg.Wait()
	c.Stop()
}
