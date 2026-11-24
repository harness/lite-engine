// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"math"
	"os"
	"runtime"
	"runtime/debug"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	metricGoroutines = "lite_engine.goroutines"
	metricOpenFDs    = "lite_engine.open_fds"
	metricGCCycles   = "lite_engine.gc.cycles"
	metricGCPause    = "lite_engine.gc.pause"
)

var (
	// Bucket bounds are fine enough to see a leak while it grows (a power-of-two
	// ladder puts 129 and 255 in the same bucket) and include the watchdog
	// limits, so the share of VMs above a limit is exact. A fleet is one label
	// set, so the extra buckets cost a handful of series in total, not per VM.
	goroutineBuckets = []float64{25, 50, 75, 100, 150, 200, 300, 400, 500, 750, 1000, 1500, 2000, 3000, 5000, 10000}
	fdBuckets        = []float64{32, 64, 96, 128, 192, 256, 384, 512, 768, 1024, 1536, 2048, 4096, 8192, 16384, 65536}

	// fdDirs are the places an open-descriptor listing can be read from, in
	// order of preference (Linux, then macOS/BSD).
	fdDirs = []string{"/proc/self/fd", "/dev/fd"}
)

// GCStats are the cumulative garbage collector counters of the process.
type GCStats struct {
	Cycles     uint64
	PauseTotal time.Duration
}

// ProcessStats is a source of process-level readings. It is an interface so
// the metrics built on it can be tested without a real, noisy process.
type ProcessStats interface {
	// Goroutines is the current number of goroutines.
	Goroutines() int
	// OpenFDs is the current number of open file descriptors; ok is false
	// when the platform cannot tell.
	OpenFDs() (count int, ok bool)
	// GC returns the cumulative GC counters.
	GC() GCStats
}

type runtimeProcessStats struct{ fdDirs []string }

var _ ProcessStats = runtimeProcessStats{}

func newRuntimeProcessStats() runtimeProcessStats { return runtimeProcessStats{fdDirs: fdDirs} }

func (runtimeProcessStats) Goroutines() int { return runtime.NumGoroutine() }

// OpenFDs counts entries of the descriptor directory. Listing the directory
// itself holds one descriptor, so the figure is exact to within one.
func (s runtimeProcessStats) OpenFDs() (int, bool) {
	for _, dir := range s.fdDirs {
		if entries, err := os.ReadDir(dir); err == nil {
			return len(entries), true
		}
	}
	return 0, false
}

func (runtimeProcessStats) GC() GCStats {
	var s debug.GCStats
	debug.ReadGCStats(&s)
	return GCStats{Cycles: uint64(max(s.NumGC, 0)), PauseTotal: s.PauseTotal}
}

// clampToInt64 converts v without wrapping to a negative number.
func clampToInt64(v uint64) int64 {
	if v > math.MaxInt64 {
		return math.MaxInt64
	}
	return int64(v)
}

// Sampler records point-in-time readings. Implementations must be safe for
// concurrent use.
type Sampler interface {
	Sample()
}

// noSampler is the Sampler for modes where readings are pulled by the SDK.
type noSampler struct{}

func (noSampler) Sample() {}

// histogramSampler records each reading into a histogram so that VMs sharing
// the same series still combine into a fleet-wide distribution.
type histogramSampler struct {
	stats      ProcessStats
	attrs      metric.MeasurementOption
	goroutines metric.Int64Histogram
	fds        metric.Int64Histogram
}

func (s *histogramSampler) Sample() {
	ctx := context.Background()
	s.goroutines.Record(ctx, int64(s.stats.Goroutines()), s.attrs)
	if n, ok := s.stats.OpenFDs(); ok {
		s.fds.Record(ctx, int64(n), s.attrs)
	}
}

// registerProcessMetrics registers GC counters and goroutine/FD readings on
// meter and returns the Sampler that feeds them.
//
// Fleet mode (gaugeMode=false) cannot use gauges: VMs sharing one series would
// overwrite each other, so goroutines and FDs are sampled into histograms.
// VM-id mode (gaugeMode=true) gives each VM its own series, where a plain
// gauge is both correct and far cheaper than per-VM histogram buckets.
func registerProcessMetrics(meter metric.Meter, base []attribute.KeyValue, stats ProcessStats, gaugeMode bool) (Sampler, error) {
	attrs := metric.WithAttributes(base...)
	if err := registerGCCounters(meter, attrs, stats); err != nil {
		return nil, err
	}
	if gaugeMode {
		return noSampler{}, registerGauges(meter, attrs, stats)
	}
	return newHistogramSampler(meter, attrs, stats)
}

func registerGCCounters(meter metric.Meter, attrs metric.MeasurementOption, stats ProcessStats) error {
	if _, err := meter.Int64ObservableCounter(metricGCCycles,
		metric.WithDescription("Garbage collection cycles completed by lite-engine."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(clampToInt64(stats.GC().Cycles), attrs)
			return nil
		})); err != nil {
		return err
	}
	_, err := meter.Float64ObservableCounter(metricGCPause,
		metric.WithDescription("Total garbage collection pause time of lite-engine."),
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, o metric.Float64Observer) error {
			o.Observe(stats.GC().PauseTotal.Seconds(), attrs)
			return nil
		}))
	return err
}

func registerGauges(meter metric.Meter, attrs metric.MeasurementOption, stats ProcessStats) error {
	if _, err := meter.Int64ObservableGauge(metricGoroutines,
		metric.WithDescription("Goroutines in lite-engine."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(stats.Goroutines()), attrs)
			return nil
		})); err != nil {
		return err
	}
	_, err := meter.Int64ObservableGauge(metricOpenFDs,
		metric.WithDescription("Open file descriptors of lite-engine."),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			if n, ok := stats.OpenFDs(); ok {
				o.Observe(int64(n), attrs)
			}
			return nil
		}))
	return err
}

func newHistogramSampler(meter metric.Meter, attrs metric.MeasurementOption, stats ProcessStats) (Sampler, error) {
	goroutines, err := meter.Int64Histogram(metricGoroutines,
		metric.WithDescription("Distribution of goroutine counts across lite-engine instances."),
		metric.WithExplicitBucketBoundaries(goroutineBuckets...))
	if err != nil {
		return nil, err
	}
	fds, err := meter.Int64Histogram(metricOpenFDs,
		metric.WithDescription("Distribution of open file descriptor counts across lite-engine instances."),
		metric.WithExplicitBucketBoundaries(fdBuckets...))
	if err != nil {
		return nil, err
	}
	return &histogramSampler{stats: stats, attrs: attrs, goroutines: goroutines, fds: fds}, nil
}

// runSampler samples once immediately (so a short-lived VM is still seen) and
// then every interval until ctx is canceled. A panicking sampler is contained.
func runSampler(ctx context.Context, s Sampler, interval time.Duration) {
	sampleOnce(s)
	tickLoop(ctx, s, interval)
}

// sampleOnce takes a single reading, containing any panic from the sampler.
// It is split out of runSampler so a caller that needs the first reading to
// be visible before it continues (the Controller, so a Flush right after
// Start deterministically observes it) can take it synchronously instead of
// racing a goroutine for it.
func sampleOnce(s Sampler) { contain(s.Sample) }

// tickLoop samples s on every tick until ctx is canceled.
func tickLoop(ctx context.Context, s Sampler, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			contain(s.Sample)
		}
	}
}
