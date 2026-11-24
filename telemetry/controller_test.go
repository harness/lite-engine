package telemetry

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// exportedPoint is one flattened data point received by fakeMetricExporter.
type exportedPoint struct {
	name  string
	attrs map[string]string
	value float64 // sum/gauge value, or histogram sum
	count uint64  // histogram count
}

// fakeMetricExporter is an in-memory sdkmetric.Exporter.
type fakeMetricExporter struct {
	selector sdkmetric.TemporalitySelector
	mu       sync.Mutex
	points   []exportedPoint
	shutdown bool
	failWith error // when set, every Export fails with it
}

func newFakeMetricExporter(cumulative bool) *fakeMetricExporter {
	return &fakeMetricExporter{selector: temporalitySelector(cumulative)}
}

func (f *fakeMetricExporter) Temporality(k sdkmetric.InstrumentKind) metricdata.Temporality {
	return f.selector(k)
}

func (f *fakeMetricExporter) Aggregation(k sdkmetric.InstrumentKind) sdkmetric.Aggregation {
	return sdkmetric.DefaultAggregationSelector(k)
}

func (f *fakeMetricExporter) Export(_ context.Context, rm *metricdata.ResourceMetrics) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.failWith != nil {
		return f.failWith
	}
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			f.points = append(f.points, flatten(m)...)
		}
	}
	return nil
}

func (f *fakeMetricExporter) ForceFlush(context.Context) error { return nil }

func (f *fakeMetricExporter) Shutdown(context.Context) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.shutdown = true
	return nil
}

func flatten(m metricdata.Metrics) []exportedPoint {
	var out []exportedPoint
	add := func(attrs map[string]string, value float64, count uint64) {
		out = append(out, exportedPoint{name: m.Name, attrs: attrs, value: value, count: count})
	}
	switch d := m.Data.(type) {
	case metricdata.Sum[int64]:
		for _, dp := range d.DataPoints {
			add(attrMap(dp.Attributes.ToSlice()), float64(dp.Value), 0)
		}
	case metricdata.Sum[float64]:
		for _, dp := range d.DataPoints {
			add(attrMap(dp.Attributes.ToSlice()), dp.Value, 0)
		}
	case metricdata.Gauge[int64]:
		for _, dp := range d.DataPoints {
			add(attrMap(dp.Attributes.ToSlice()), float64(dp.Value), 0)
		}
	case metricdata.Histogram[float64]:
		for _, dp := range d.DataPoints {
			add(attrMap(dp.Attributes.ToSlice()), dp.Sum, dp.Count)
		}
	case metricdata.Histogram[int64]:
		for _, dp := range d.DataPoints {
			add(attrMap(dp.Attributes.ToSlice()), float64(dp.Sum), dp.Count)
		}
	}
	return out
}

func (f *fakeMetricExporter) find(name string, match map[string]string) (exportedPoint, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.points {
		if p.name != name {
			continue
		}
		ok := true
		for k, v := range match {
			if p.attrs[k] != v {
				ok = false
			}
		}
		if ok {
			return p, true
		}
	}
	return exportedPoint{}, false
}

func (f *fakeMetricExporter) isShutdown() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.shutdown
}

// controllerHarness is a Controller wired to in-memory exporters and a private logrus logger.
type controllerHarness struct {
	ctrl    *Controller
	metrics *fakeMetricExporter
	logs    *captureExporter
	logger  *logrus.Logger
	stats   *fakeProcessStats
}

func newControllerHarness(t *testing.T, cumulative bool) *controllerHarness {
	t.Helper()
	h := &controllerHarness{
		metrics: newFakeMetricExporter(cumulative),
		logs:    &captureExporter{},
		logger:  logrus.New(),
		stats:   &fakeProcessStats{goroutines: 42, fds: 17, fdsOK: true},
	}
	h.logger.SetOutput(discard{})
	h.ctrl = NewController(
		withMetricExporter(func(Config) (sdkmetric.Exporter, error) { return h.metrics, nil }),
		withLogExporter(func(Config) (sdklog.Exporter, error) { return h.logs, nil }),
		withProcessStats(h.stats),
		withLogger(h.logger),
		withHostname(func() (string, error) { return "vm-9", nil }),
		withVersion("v9.9.9"),
	)
	t.Cleanup(func() { _ = h.ctrl.Shutdown(context.Background()) })
	return h
}

func enabledConfig() Config {
	return Config{
		Enabled:       true,
		Endpoint:      "http://127.0.0.1:1",
		MetricLabels:  map[string]string{testPoolLabel: testPool},
		Interval:      time.Hour, // exports happen only when a test flushes
		ExportTimeout: time.Second,
	}
}

// bufferWriter collects what a logrus logger prints, safely across goroutines.
type bufferWriter struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *bufferWriter) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *bufferWriter) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func hookCount(l *logrus.Logger) int { return len(l.Hooks[logrus.InfoLevel]) }

func TestController_DisabledConfigStartsNothing(t *testing.T) {
	h := newControllerHarness(t, false)

	require.NoError(t, h.ctrl.Start(Config{Enabled: false, Endpoint: testEndpoint}))

	assert.Zero(t, hookCount(h.logger), "a disabled telemetry must not touch logging")
	h.ctrl.Recorder().StepStarted() // must still be safe
	require.NoError(t, h.ctrl.Flush(context.Background()))
}

func TestController_InvalidConfigIsAnErrorAndStartsNothing(t *testing.T) {
	h := newControllerHarness(t, false)
	bad := enabledConfig()
	bad.Endpoint = ""

	require.Error(t, h.ctrl.Start(bad))

	assert.Zero(t, hookCount(h.logger))
	require.NoError(t, h.ctrl.Start(enabledConfig()), "a later, valid setup must still be able to start telemetry")
	assert.Equal(t, 1, hookCount(h.logger))
}

func TestController_RecorderCapturedBeforeStartForwardsAfterStart(t *testing.T) {
	h := newControllerHarness(t, false)
	early := h.ctrl.Recorder() // what a client constructed before /setup would hold
	early.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)

	require.NoError(t, h.ctrl.Start(enabledConfig()))
	early.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, errors.New("x"))
	require.NoError(t, h.ctrl.Flush(context.Background()))

	p, found := h.metrics.find("lite_engine.calls", map[string]string{"subsystem": "logsvc", "op": "write", wireOutcomeAttr: "error"})
	require.True(t, found)
	assert.Equal(t, float64(1), p.value)
	_, found = h.metrics.find("lite_engine.calls", map[string]string{wireOutcomeAttr: "success"})
	assert.False(t, found, "the call made before Start was not recorded")
}

func TestController_FleetModeMetricsCarryNoVMIdentity(t *testing.T) {
	h := newControllerHarness(t, false)
	require.NoError(t, h.ctrl.Start(enabledConfig()))

	h.ctrl.Recorder().ObserveCall(SubsystemTIService, OpTIReport, time.Millisecond, nil)
	require.NoError(t, h.ctrl.Flush(context.Background()))

	p, found := h.metrics.find("lite_engine.calls", map[string]string{"subsystem": "ti"})
	require.True(t, found)
	assert.NotContains(t, p.attrs, "vm_id")
	assert.Equal(t, testPool, p.attrs[testPoolLabel])
	assert.Equal(t, "v9.9.9", p.attrs["version"])
}

func TestController_FleetModeSamplesProcessHistogramsOnStart(t *testing.T) {
	h := newControllerHarness(t, false)
	require.NoError(t, h.ctrl.Start(enabledConfig()))

	require.NoError(t, h.ctrl.Flush(context.Background()))

	p, found := h.metrics.find("lite_engine.goroutines", nil)
	require.True(t, found, "a VM that dies before the first tick must still be sampled once")
	assert.Equal(t, float64(42), p.value)
	assert.Equal(t, uint64(1), p.count)
}

func TestController_VMIDModeLabelsMetricsAndExportsZeroBaselinesImmediately(t *testing.T) {
	h := newControllerHarness(t, true)
	cfg := enabledConfig()
	cfg.MetricsVMID = true

	require.NoError(t, h.ctrl.Start(cfg)) // note: no Flush

	require.Eventually(t, func() bool {
		p, found := h.metrics.find("lite_engine.calls", map[string]string{"subsystem": "logsvc", "op": "write", wireOutcomeAttr: "error", "vm_id": "vm-9"})
		return found && p.value == 0
	}, 5*time.Second, 5*time.Millisecond, "zero baselines must reach the backend before any real event")
}

func TestController_ShipsLogsWithVMIdentity(t *testing.T) {
	h := newControllerHarness(t, false)
	require.NoError(t, h.ctrl.Start(enabledConfig()))

	h.logger.WithField("step", "build").Info("hello")
	h.logger.Debug("below the default level")
	require.NoError(t, h.ctrl.Flush(context.Background()))

	logs := h.logs.snapshot()
	require.Len(t, logs, 1)
	assert.Equal(t, "hello", logs[0].body)
	assert.Equal(t, "vm-9", logs[0].attrs["vm_id"])
	assert.Equal(t, "build", logs[0].attrs["step"])
}

func TestController_StartIsIdempotentAndFirstConfigWins(t *testing.T) {
	h := newControllerHarness(t, false)
	other := enabledConfig()
	other.MetricLabels = map[string]string{testPoolLabel: "second"}

	require.NoError(t, h.ctrl.Start(enabledConfig()))
	require.NoError(t, h.ctrl.Start(other))

	assert.Equal(t, 1, hookCount(h.logger), "a replayed /setup must not stack hooks")
	h.ctrl.Recorder().StepStarted()
	require.NoError(t, h.ctrl.Flush(context.Background()))
	p, found := h.metrics.find("lite_engine.steps.started", nil)
	require.True(t, found)
	assert.Equal(t, testPool, p.attrs[testPoolLabel])
}

func TestController_ShutdownFlushesPendingDataAndDetaches(t *testing.T) {
	h := newControllerHarness(t, false)
	require.NoError(t, h.ctrl.Start(enabledConfig()))
	h.ctrl.Recorder().StepStarted()
	h.logger.Info("last words")

	require.NoError(t, h.ctrl.Shutdown(context.Background()))

	_, found := h.metrics.find("lite_engine.steps.started", nil)
	assert.True(t, found, "data recorded before shutdown must be delivered")
	assert.Len(t, h.logs.snapshot(), 1)
	assert.True(t, h.metrics.isShutdown())

	require.NotPanics(t, func() {
		h.ctrl.Recorder().StepStarted()
		h.logger.Info("after shutdown")
	})
	assert.Len(t, h.logs.snapshot(), 1, "logging after shutdown ships nothing")
}

func TestController_ShutdownIsIdempotentAndBlocksRestart(t *testing.T) {
	h := newControllerHarness(t, false)
	require.NoError(t, h.ctrl.Start(enabledConfig()))

	require.NoError(t, h.ctrl.Shutdown(context.Background()))
	require.NoError(t, h.ctrl.Shutdown(context.Background()))

	assert.ErrorIs(t, h.ctrl.Start(enabledConfig()), ErrClosed)
}

func TestController_ShutdownAndFlushBeforeStartAreHarmless(t *testing.T) {
	h := newControllerHarness(t, false)

	require.NoError(t, h.ctrl.Flush(context.Background()))
	require.NoError(t, h.ctrl.Shutdown(context.Background()))
}

func TestController_ExporterConstructionFailureIsAnErrorNotAPanic(t *testing.T) {
	h := newControllerHarness(t, false)
	h.ctrl = NewController(
		withMetricExporter(func(Config) (sdkmetric.Exporter, error) { return nil, errors.New("tls misconfigured") }),
		withLogger(h.logger),
	)

	err := h.ctrl.Start(enabledConfig())

	require.Error(t, err)
	assert.Zero(t, hookCount(h.logger), "a half-started telemetry must leave no hook behind")
	require.NoError(t, h.ctrl.Shutdown(context.Background()))
}

// Race: Start, Flush, Shutdown, recording and logging all happen at once -
// the worst case for a setup request arriving while steps already run and the
// server is shutting down. Run with -race.
func TestController_ConcurrentLifecycleAndTrafficIsRaceFree(t *testing.T) {
	h := newControllerHarness(t, false)
	rec := h.ctrl.Recorder()
	cfg := enabledConfig()

	var wg sync.WaitGroup
	run := func(n int, fn func()) {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range n {
				fn()
			}
		}()
	}
	for range 4 {
		run(200, func() { rec.ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil) })
		run(100, func() { rec.StepStarted(); rec.StepFinished() })
		run(100, func() { h.logger.Info("traffic") })
	}
	run(5, func() { _ = h.ctrl.Start(cfg) })
	run(5, func() { _ = h.ctrl.Flush(context.Background()) })
	run(1, func() { time.Sleep(5 * time.Millisecond); _ = h.ctrl.Shutdown(context.Background()) })
	wg.Wait()

	require.NoError(t, h.ctrl.Shutdown(context.Background()))
	assert.ErrorIs(t, h.ctrl.Start(cfg), ErrClosed)
}

func attrMap(kvs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.String()
	}
	return m
}

// A failing periodic export is reported through logrus, which feeds the log
// hook. The report must stay local: shipping it would re-trigger the failing
// export.
func TestController_PeriodicExportFailuresAreLoggedLocallyAndNeverShippedBack(t *testing.T) {
	h := newControllerHarness(t, false)
	out := &bufferWriter{}
	h.logger.SetOutput(out)
	h.metrics.failWith = errors.New("backend on fire")
	cfg := enabledConfig()
	cfg.Interval = 20 * time.Millisecond
	require.NoError(t, h.ctrl.Start(cfg))
	h.ctrl.Recorder().StepStarted()

	require.Eventually(t, func() bool { return strings.Contains(out.String(), "OTLP export failed") },
		5*time.Second, 10*time.Millisecond, "the operator must be able to see the failure in lite-engine's own log")

	_ = h.ctrl.Flush(context.Background()) // push any buffered log records through
	for _, l := range h.logs.snapshot() {
		assert.NotContains(t, l.body, "OTLP export failed", "telemetry diagnostics must never be exported")
	}
}
