package telemetry

import (
	"context"
	"errors"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// metricsHarness wires a metricsRecorder to an in-memory reader so tests assert
// on exactly what would be exported.
type metricsHarness struct {
	reader *sdkmetric.ManualReader
	rec    *metricsRecorder
}

func newMetricsHarness(t *testing.T, cfg Config) *metricsHarness {
	t.Helper()
	cumulative := cfg.MetricsVMID
	reader := sdkmetric.NewManualReader(sdkmetric.WithTemporalitySelector(temporalitySelector(cumulative)))
	provider := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	rec, err := newMetricsRecorder(provider.Meter("test"), staticAttributes(cfg, "vm-1", "v1.2.3"), cumulative)
	require.NoError(t, err)
	return &metricsHarness{reader: reader, rec: rec}
}

func (h *metricsHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	return rm
}

func findMetric(rm metricdata.ResourceMetrics, name string) (metricdata.Metrics, bool) {
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name == name {
				return m, true
			}
		}
	}
	return metricdata.Metrics{}, false
}

// sumValue returns the value of the int64 sum data point carrying exactly attrs.
func sumValue(t *testing.T, rm metricdata.ResourceMetrics, name string, attrs ...attribute.KeyValue) (int64, bool) {
	t.Helper()
	m, ok := findMetric(rm, name)
	if !ok {
		return 0, false
	}
	sum, ok := m.Data.(metricdata.Sum[int64])
	require.True(t, ok, "%s must be an int64 sum", name)
	want := attribute.NewSet(attrs...)
	for _, dp := range sum.DataPoints {
		if dp.Attributes.Equals(&want) {
			return dp.Value, true
		}
	}
	return 0, false
}

func fleetConfig() Config {
	return Config{Enabled: true, Endpoint: testEndpoint, MetricLabels: map[string]string{testPoolLabel: testPool}}
}

func baseAttrs(extra ...attribute.KeyValue) []attribute.KeyValue {
	return append([]attribute.KeyValue{
		attribute.String(testPoolLabel, testPool),
		attribute.String("version", "v1.2.3"),
		attribute.String("os", runtime.GOOS),
		attribute.String("arch", runtime.GOARCH),
	}, extra...)
}

func TestMetricsRecorder_CountsCallsByOutcome(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())

	h.rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
	h.rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
	h.rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, errors.New("503"))
	h.rec.ObserveCall(SubsystemTIService, OpTIReport, time.Millisecond, errors.New("timeout"))
	rm := h.collect(t)

	ok, found := sumValue(t, rm, "lite_engine.calls",
		baseAttrs(attribute.String("subsystem", wireLogsvc), attribute.String("op", "write"), attribute.String(wireOutcomeAttr, "success"))...)
	require.True(t, found)
	assert.Equal(t, int64(2), ok)

	failed, found := sumValue(t, rm, "lite_engine.calls",
		baseAttrs(attribute.String("subsystem", wireLogsvc), attribute.String("op", "write"), attribute.String(wireOutcomeAttr, wireOutcomeError))...)
	require.True(t, found)
	assert.Equal(t, int64(1), failed)

	tiFailed, found := sumValue(t, rm, "lite_engine.calls",
		baseAttrs(attribute.String("subsystem", "ti"), attribute.String("op", "report"), attribute.String(wireOutcomeAttr, wireOutcomeError))...)
	require.True(t, found)
	assert.Equal(t, int64(1), tiFailed)
}

func TestMetricsRecorder_RecordsDurationInSecondsByOp(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())

	h.rec.ObserveCall(SubsystemTIService, OpTIUploadCallgraph, 250*time.Millisecond, errors.New("x"))
	rm := h.collect(t)

	m, found := findMetric(rm, "lite_engine.call.duration")
	require.True(t, found)
	assert.Equal(t, "s", m.Unit)
	hist, ok := m.Data.(metricdata.Histogram[float64])
	require.True(t, ok)
	require.Len(t, hist.DataPoints, 1)
	dp := hist.DataPoints[0]

	want := attribute.NewSet(baseAttrs(attribute.String("subsystem", "ti"), attribute.String("op", "upload_callgraph"))...)
	assert.True(t, dp.Attributes.Equals(&want), "duration is dimensioned by subsystem+op only, never by outcome: %v", dp.Attributes)
	assert.Equal(t, uint64(1), dp.Count)
	assert.InDelta(t, 0.25, dp.Sum, 1e-9)
	idx := -1
	for i, b := range dp.Bounds {
		if b == 0.25 {
			idx = i
		}
	}
	require.GreaterOrEqual(t, idx, 0, "a 0.25s bucket boundary must exist")
	assert.Equal(t, uint64(1), dp.BucketCounts[idx], "250ms belongs in the le=0.25 bucket")
}

func TestMetricsRecorder_CountsStepStartsAndFinishes(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())

	h.rec.StepStarted()
	h.rec.StepStarted()
	h.rec.StepFinished()
	rm := h.collect(t)

	started, found := sumValue(t, rm, "lite_engine.steps.started", baseAttrs()...)
	require.True(t, found)
	assert.Equal(t, int64(2), started)
	finished, found := sumValue(t, rm, "lite_engine.steps.finished", baseAttrs()...)
	require.True(t, found)
	assert.Equal(t, int64(1), finished)
}

func TestStaticAttributes_VMIDOnlyWhenFlagged(t *testing.T) {
	off := attribute.NewSet(staticAttributes(fleetConfig(), "vm-1", "v1")...)
	_, hasVMID := off.Value("vm_id")
	assert.False(t, hasVMID, "default must be fleet-level: no per-VM label")

	cfg := fleetConfig()
	cfg.MetricsVMID = true
	on := attribute.NewSet(staticAttributes(cfg, "vm-1", "v1")...)
	v, hasVMID := on.Value("vm_id")
	require.True(t, hasVMID)
	assert.Equal(t, "vm-1", v.AsString())
}

func TestMetricsRecorder_DefaultModeEmitsDeltas(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())
	attrs := baseAttrs(attribute.String("subsystem", wireLogsvc), attribute.String("op", "open"), attribute.String(wireOutcomeAttr, "success"))

	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	first, _ := sumValue(t, h.collect(t), "lite_engine.calls", attrs...)
	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	second, _ := sumValue(t, h.collect(t), "lite_engine.calls", attrs...)

	assert.Equal(t, int64(1), first)
	assert.Equal(t, int64(2), second, "delta temporality reports the change since the last export, not the running total")
}

func TestMetricsRecorder_DefaultModeHasNoBaselines(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())

	rm := h.collect(t)

	_, found := findMetric(rm, "lite_engine.calls")
	assert.False(t, found, "delta mode must not emit series for calls that never happened")
}

func TestMetricsRecorder_VMIDModeEmitsCumulativeTotals(t *testing.T) {
	cfg := fleetConfig()
	cfg.MetricsVMID = true
	h := newMetricsHarness(t, cfg)
	attrs := baseAttrs(attribute.String("vm_id", "vm-1"),
		attribute.String("subsystem", wireLogsvc), attribute.String("op", "open"), attribute.String(wireOutcomeAttr, "success"))

	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	_ = h.collect(t)
	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	h.rec.ObserveCall(SubsystemLogService, OpLogOpen, time.Millisecond, nil)
	total, found := sumValue(t, h.collect(t), "lite_engine.calls", attrs...)

	require.True(t, found)
	assert.Equal(t, int64(3), total)
}

// With a per-VM label every series is born on the VM's first event, and
// rate()/increase() cannot see a counter's first value. Zero baselines make
// the series exist (at 0) before anything happens.
func TestMetricsRecorder_VMIDModeSeedsZeroBaselinesForEveryKnownSeries(t *testing.T) {
	cfg := fleetConfig()
	cfg.MetricsVMID = true
	h := newMetricsHarness(t, cfg)

	rm := h.collect(t)

	for _, tc := range []struct{ sub, op string }{
		{wireLogsvc, "open"}, {wireLogsvc, "write"}, {wireLogsvc, "close"}, {wireLogsvc, "upload"},
		{"ti", "report"}, {"ti", "upload_callgraph"},
	} {
		for _, outcome := range []string{"success", wireOutcomeError} {
			v, found := sumValue(t, rm, "lite_engine.calls", baseAttrs(attribute.String("vm_id", "vm-1"),
				attribute.String("subsystem", tc.sub), attribute.String("op", tc.op), attribute.String(wireOutcomeAttr, outcome))...)
			assert.True(t, found, "missing baseline %s/%s/%s", tc.sub, tc.op, outcome)
			assert.Equal(t, int64(0), v)
		}
	}
	for _, name := range []string{"lite_engine.steps.started", "lite_engine.steps.finished"} {
		v, found := sumValue(t, rm, name, baseAttrs(attribute.String("vm_id", "vm-1"))...)
		assert.True(t, found, "missing baseline for %s", name)
		assert.Equal(t, int64(0), v)
	}
}

// Race: every recorder method is hit concurrently with collection (the export
// goroutine). Run with -race.
func TestMetricsRecorder_ConcurrentRecordingAndCollectionIsRaceFree(t *testing.T) {
	h := newMetricsHarness(t, fleetConfig())

	const workers, iterations = 8, 500
	var wg sync.WaitGroup
	for range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range iterations {
				h.rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
				h.rec.StepStarted()
				h.rec.StepFinished()
			}
		}()
	}
	stop := make(chan struct{})
	collected := make(chan struct{})
	go func() {
		defer close(collected)
		for {
			select {
			case <-stop:
				return
			default:
				h.collect(t)
			}
		}
	}()
	wg.Wait()
	close(stop)
	<-collected

	// Delta mode: totals across all collections equal what was recorded; here we
	// only need the final drain to be consistent and non-negative.
	rm := h.collect(t)
	if v, found := sumValue(t, rm, "lite_engine.steps.started", baseAttrs()...); found {
		assert.GreaterOrEqual(t, v, int64(0))
	}
}
