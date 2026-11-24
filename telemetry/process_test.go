package telemetry

import (
	"context"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// fakeProcessStats is a ProcessStats whose readings tests control.
type fakeProcessStats struct {
	mu         sync.Mutex
	goroutines int
	fds        int
	fdsOK      bool
	gc         GCStats
}

func (f *fakeProcessStats) set(fn func(*fakeProcessStats)) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn(f)
}

func (f *fakeProcessStats) Goroutines() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.goroutines
}

func (f *fakeProcessStats) OpenFDs() (int, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.fds, f.fdsOK
}

func (f *fakeProcessStats) GC() GCStats {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.gc
}

type processHarness struct {
	reader  *sdkmetric.ManualReader
	sampler Sampler
	stats   *fakeProcessStats
}

func newProcessHarness(t *testing.T, vmIDMode bool) *processHarness {
	t.Helper()
	reader := sdkmetric.NewManualReader(sdkmetric.WithTemporalitySelector(temporalitySelector(vmIDMode)))
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	stats := &fakeProcessStats{goroutines: 42, fds: 17, fdsOK: true}
	cfg := fleetConfig()
	cfg.MetricsVMID = vmIDMode
	sampler, err := registerProcessMetrics(provider.Meter("test"), staticAttributes(cfg, "vm-1", "v1.2.3"), stats, vmIDMode)
	require.NoError(t, err)
	return &processHarness{reader: reader, sampler: sampler, stats: stats}
}

func (h *processHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	return rm
}

func histogramPoint(t *testing.T, rm metricdata.ResourceMetrics, name string) (metricdata.HistogramDataPoint[int64], bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return metricdata.HistogramDataPoint[int64]{}, false
	}
	hist, ok := m.Data.(metricdata.Histogram[int64])
	require.True(t, ok, "%s must be an int64 histogram", name)
	require.Len(t, hist.DataPoints, 1)
	return hist.DataPoints[0], true
}

func gaugeValue(t *testing.T, rm metricdata.ResourceMetrics, name string) (int64, attribute.Set, bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return 0, attribute.Set{}, false
	}
	g, ok := m.Data.(metricdata.Gauge[int64])
	require.True(t, ok, "%s must be an int64 gauge", name)
	require.Len(t, g.DataPoints, 1)
	return g.DataPoints[0].Value, g.DataPoints[0].Attributes, true
}

func TestFleetMode_SamplesGoroutinesAndFDsIntoHistograms(t *testing.T) {
	h := newProcessHarness(t, false)

	h.sampler.Sample()
	rm := h.collect(t)

	g, found := histogramPoint(t, rm, "lite_engine.goroutines")
	require.True(t, found)
	assert.Equal(t, uint64(1), g.Count)
	assert.Equal(t, int64(42), g.Sum)
	fds, found := histogramPoint(t, rm, "lite_engine.open_fds")
	require.True(t, found)
	assert.Equal(t, int64(17), fds.Sum)

	want := attribute.NewSet(baseAttrs()...)
	assert.True(t, g.Attributes.Equals(&want), "fleet histograms carry only fleet labels: %v", g.Attributes)
}

func TestFleetMode_SkipsFDsWhenTheyCannotBeRead(t *testing.T) {
	h := newProcessHarness(t, false)
	h.stats.set(func(f *fakeProcessStats) { f.fdsOK = false })

	h.sampler.Sample()
	rm := h.collect(t)

	_, found := histogramPoint(t, rm, "lite_engine.open_fds")
	assert.False(t, found, "an unreadable FD count must not be reported as 0")
	_, found = histogramPoint(t, rm, "lite_engine.goroutines")
	assert.True(t, found, "other readings are unaffected")
}

func TestFleetMode_RecordsNothingUntilSampled(t *testing.T) {
	h := newProcessHarness(t, false)

	rm := h.collect(t)

	_, found := findMetric(rm, "lite_engine.goroutines")
	assert.False(t, found)
}

func TestVMIDMode_ExposesGoroutinesAndFDsAsPerVMGauges(t *testing.T) {
	h := newProcessHarness(t, true)

	rm := h.collect(t)

	g, attrs, found := gaugeValue(t, rm, "lite_engine.goroutines")
	require.True(t, found)
	assert.Equal(t, int64(42), g)
	v, hasVMID := attrs.Value("vm_id")
	require.True(t, hasVMID)
	assert.Equal(t, "vm-1", v.AsString())
	fds, _, found := gaugeValue(t, rm, "lite_engine.open_fds")
	require.True(t, found)
	assert.Equal(t, int64(17), fds)
}

func TestVMIDMode_OmitsFDGaugeWhenUnreadable(t *testing.T) {
	h := newProcessHarness(t, true)
	h.stats.set(func(f *fakeProcessStats) { f.fdsOK = false })

	rm := h.collect(t)

	_, _, found := gaugeValue(t, rm, "lite_engine.open_fds")
	assert.False(t, found)
}

func TestVMIDMode_SamplingIsANoOp(t *testing.T) {
	h := newProcessHarness(t, true)

	require.NotPanics(t, h.sampler.Sample)
}

func TestGCCounters_ReportCyclesAndPauseSecondsAsDeltasInFleetMode(t *testing.T) {
	h := newProcessHarness(t, false)
	h.stats.set(func(f *fakeProcessStats) { f.gc = GCStats{Cycles: 5, PauseTotal: 1500 * time.Millisecond} })

	first := h.collect(t)
	h.stats.set(func(f *fakeProcessStats) { f.gc = GCStats{Cycles: 8, PauseTotal: 2 * time.Second} })
	second := h.collect(t)

	attrs := baseAttrs()
	cycles1, _ := sumValue(t, first, "lite_engine.gc.cycles", attrs...)
	cycles2, _ := sumValue(t, second, "lite_engine.gc.cycles", attrs...)
	assert.Equal(t, int64(5), cycles1)
	assert.Equal(t, int64(3), cycles2, "delta since the previous export")

	pause, found := findMetric(second, "lite_engine.gc.pause")
	require.True(t, found)
	assert.Equal(t, "s", pause.Unit)
	sum, ok := pause.Data.(metricdata.Sum[float64])
	require.True(t, ok)
	require.Len(t, sum.DataPoints, 1)
	assert.InDelta(t, 0.5, sum.DataPoints[0].Value, 1e-9)
}

func TestGCCounters_AreCumulativeInVMIDMode(t *testing.T) {
	h := newProcessHarness(t, true)
	h.stats.set(func(f *fakeProcessStats) { f.gc = GCStats{Cycles: 5} })
	_ = h.collect(t)
	h.stats.set(func(f *fakeProcessStats) { f.gc = GCStats{Cycles: 8} })

	rm := h.collect(t)

	v, found := sumValue(t, rm, "lite_engine.gc.cycles", baseAttrs(attribute.String("vm_id", "vm-1"))...)
	require.True(t, found)
	assert.Equal(t, int64(8), v)
}

// Characterization of the real source: it must read the live process.
func TestRuntimeProcessStats_ReadsTheLiveProcess(t *testing.T) {
	stats := newRuntimeProcessStats()

	assert.GreaterOrEqual(t, stats.Goroutines(), 1)

	before := stats.GC().Cycles
	runtime.GC()
	assert.Greater(t, stats.GC().Cycles, before)
	assert.Greater(t, stats.GC().PauseTotal, time.Duration(0))
}

func TestRuntimeProcessStats_CountsOpenFileDescriptors(t *testing.T) {
	stats := newRuntimeProcessStats()
	before, ok := stats.OpenFDs()
	if !ok {
		t.Skipf("fd counting unsupported on %s", runtime.GOOS)
	}

	f, err := os.Open(os.DevNull)
	require.NoError(t, err)
	defer f.Close()

	after, ok := stats.OpenFDs()
	require.True(t, ok)
	assert.Greater(t, after, before)
}

type countingSampler struct{ n atomic.Int64 }

func (c *countingSampler) Sample() { c.n.Add(1) }

func TestRunSampler_SamplesImmediatelyThenOnEveryTick(t *testing.T) {
	s := &countingSampler{}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() { defer close(done); runSampler(ctx, s, 10*time.Millisecond) }()

	require.Eventually(t, func() bool { return s.n.Load() >= 3 }, 2*time.Second, time.Millisecond,
		"an immediate sample plus periodic ones")
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("runSampler must return when its context is canceled")
	}
}

func TestRunSampler_ShortLivedProcessesStillGetOneSample(t *testing.T) {
	s := &countingSampler{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // canceled before the first tick could ever fire

	runSampler(ctx, s, time.Hour)

	assert.Equal(t, int64(1), s.n.Load())
}

type panickingSampler struct{}

func (panickingSampler) Sample() { panic("boom") }

func TestRunSampler_ASamplerPanicDoesNotEscape(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	require.NotPanics(t, func() { runSampler(ctx, panickingSampler{}, 5*time.Millisecond) })
}

// Race: sampling and collection run concurrently while readings change.
func TestSampling_ConcurrentSamplesAndCollectionAreRaceFree(t *testing.T) {
	h := newProcessHarness(t, false)

	var wg sync.WaitGroup
	for range 4 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range 300 {
				h.stats.set(func(f *fakeProcessStats) { f.goroutines = i })
				h.sampler.Sample()
			}
		}()
	}
	wg.Add(1)
	go func() {
		defer wg.Done()
		for range 100 {
			h.collect(t)
		}
	}()
	wg.Wait()
}
