// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package osstats

import (
	"context"
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
