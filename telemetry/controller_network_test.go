package telemetry

import (
	"compress/gzip"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	collogs "go.opentelemetry.io/proto/otlp/collector/logs/v1"
	colmetrics "go.opentelemetry.io/proto/otlp/collector/metrics/v1"
	commonpb "go.opentelemetry.io/proto/otlp/common/v1"
	metricspb "go.opentelemetry.io/proto/otlp/metrics/v1"
	"google.golang.org/protobuf/proto"
)

// otlpReceiver is a real OTLP/HTTP endpoint that decodes what lite-engine
// actually puts on the wire.
type otlpReceiver struct {
	server  *httptest.Server
	mu      sync.Mutex
	metrics []*colmetrics.ExportMetricsServiceRequest
	logs    []*collogs.ExportLogsServiceRequest
	paths   []string
}

func newOTLPReceiver(t *testing.T) *otlpReceiver {
	t.Helper()
	r := &otlpReceiver{}
	r.server = httptest.NewServer(http.HandlerFunc(r.handle))
	t.Cleanup(r.server.Close)
	return r
}

func (r *otlpReceiver) handle(w http.ResponseWriter, req *http.Request) {
	body := req.Body
	if req.Header.Get("Content-Encoding") == "gzip" {
		gz, err := gzip.NewReader(req.Body)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		body = gz
	}
	raw, err := io.ReadAll(body)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.paths = append(r.paths, req.URL.Path)
	switch req.URL.Path {
	case "/v1/metrics":
		var m colmetrics.ExportMetricsServiceRequest
		if proto.Unmarshal(raw, &m) == nil {
			r.metrics = append(r.metrics, &m)
		}
	case "/v1/logs":
		var l collogs.ExportLogsServiceRequest
		if proto.Unmarshal(raw, &l) == nil {
			r.logs = append(r.logs, &l)
		}
	}
	w.Header().Set("Content-Type", "application/x-protobuf")
	w.WriteHeader(http.StatusOK)
}

func (r *otlpReceiver) metric(name string) (*metricspb.Metric, *commonpb.KeyValueList, bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, req := range r.metrics {
		for _, rm := range req.ResourceMetrics {
			for _, sm := range rm.ScopeMetrics {
				for _, m := range sm.Metrics {
					if m.Name == name {
						res := &commonpb.KeyValueList{Values: rm.Resource.Attributes}
						return m, res, true
					}
				}
			}
		}
	}
	return nil, nil, false
}

func pbAttrs(kvs []*commonpb.KeyValue) map[string]string {
	out := map[string]string{}
	for _, kv := range kvs {
		out[kv.Key] = kv.Value.GetStringValue()
	}
	return out
}

func networkController(t *testing.T, logger *logrus.Logger) *Controller {
	t.Helper()
	c := NewController(withLogger(logger), withHostname(func() (string, error) { return "vm-net", nil }), withVersion("v0.0.1"))
	t.Cleanup(func() { _ = c.Shutdown(context.Background()) })
	return c
}

func networkConfig(endpoint string) Config {
	return Config{
		Enabled:       true,
		Endpoint:      endpoint,
		MetricLabels:  map[string]string{testPoolLabel: testPool},
		Interval:      time.Hour,
		ExportTimeout: 500 * time.Millisecond,
	}
}

func quietLogger() *logrus.Logger {
	l := logrus.New()
	l.SetOutput(discard{})
	return l
}

func TestNetwork_FleetModeSendsDeltaMetricsWithoutVMIdentity(t *testing.T) {
	rx := newOTLPReceiver(t)
	c := networkController(t, quietLogger())
	require.NoError(t, c.Start(networkConfig(rx.server.URL)))

	c.Recorder().ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
	require.NoError(t, c.Flush(context.Background()))

	m, res, found := rx.metric("lite_engine.calls")
	require.True(t, found, "paths seen: %v", rx.paths)
	sum := m.GetSum()
	require.NotNil(t, sum)
	assert.Equal(t, metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_DELTA, sum.AggregationTemporality)
	require.Len(t, sum.DataPoints, 1)
	attrs := pbAttrs(sum.DataPoints[0].Attributes)
	assert.NotContains(t, attrs, "vm_id")
	assert.Equal(t, testPool, attrs[testPoolLabel])
	assert.Equal(t, "write", attrs["op"])

	resAttrs := pbAttrs(res.Values)
	assert.Equal(t, "lite-engine", resAttrs["service.name"])
	for k := range resAttrs {
		assert.NotContains(t, []string{"host.name", "service.instance.id", "host.id"}, k, "resource must not identify a VM")
	}
}

func TestNetwork_VMIDModeSendsCumulativeMetricsLabelledWithTheVM(t *testing.T) {
	rx := newOTLPReceiver(t)
	c := networkController(t, quietLogger())
	cfg := networkConfig(rx.server.URL)
	cfg.MetricsVMID = true
	require.NoError(t, c.Start(cfg))

	c.Recorder().ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
	require.NoError(t, c.Flush(context.Background()))

	m, _, found := rx.metric("lite_engine.calls")
	require.True(t, found)
	sum := m.GetSum()
	assert.Equal(t, metricspb.AggregationTemporality_AGGREGATION_TEMPORALITY_CUMULATIVE, sum.AggregationTemporality)
	var sawVM bool
	for _, dp := range sum.DataPoints {
		sawVM = sawVM || pbAttrs(dp.Attributes)["vm_id"] == "vm-net"
	}
	assert.True(t, sawVM)
}

func TestNetwork_LogsArriveOnTheLogsEndpointWithVMIdentity(t *testing.T) {
	rx := newOTLPReceiver(t)
	logger := quietLogger()
	c := networkController(t, logger)
	require.NoError(t, c.Start(networkConfig(rx.server.URL)))

	logger.WithField("step", "build").Info("hello over the wire")
	require.NoError(t, c.Flush(context.Background()))

	rx.mu.Lock()
	defer rx.mu.Unlock()
	require.NotEmpty(t, rx.logs)
	rec := rx.logs[0].ResourceLogs[0].ScopeLogs[0].LogRecords[0]
	assert.Equal(t, "hello over the wire", rec.Body.GetStringValue())
	attrs := pbAttrs(rec.Attributes)
	assert.Equal(t, "vm-net", attrs["vm_id"])
	assert.Equal(t, "build", attrs["step"])
}

// closedPortURL returns an endpoint where nothing listens (connection refused).
func closedPortURL(t *testing.T) string {
	t.Helper()
	l, err := (&net.ListenConfig{}).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	url := "http://" + l.Addr().String()
	require.NoError(t, l.Close())
	return url
}

// blackholeURL returns an endpoint that accepts requests and never answers.
func blackholeURL(t *testing.T) string {
	t.Helper()
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { <-release }))
	t.Cleanup(func() { close(release); srv.Close() })
	return srv.URL
}

// The fail-open contract, exercised end to end against endpoints that are
// down or hung: telemetry must never fail, block or noticeably slow the
// operations it observes, and its own lifecycle calls must be bounded.
func TestNetwork_UnreachableEndpointNeverHurtsTheHostProcess(t *testing.T) {
	endpoints := map[string]func(*testing.T) string{
		"connection refused": closedPortURL,
		"accepts and hangs":  blackholeURL,
	}
	for name, endpoint := range endpoints {
		t.Run(name, func(t *testing.T) {
			url := endpoint(t)
			logger := quietLogger()
			c := networkController(t, logger)
			cfg := networkConfig(url)
			cfg.MetricsVMID = true // also the mode that exports baselines in the background

			assertWithin(t, 250*time.Millisecond, "Start", func() { require.NoError(t, c.Start(cfg)) })

			assertWithin(t, 2*time.Second, "10k recorded calls and 10k log lines", func() {
				for range 10000 {
					c.Recorder().ObserveCall(SubsystemLogService, OpLogWrite, time.Millisecond, nil)
					logger.Info("busy")
				}
			})

			assertWithin(t, 3*time.Second, "Flush", func() {
				assert.Error(t, c.Flush(context.Background()), "an unreachable backend is reported, not hidden")
			})
			assertWithin(t, 3*time.Second, "Shutdown", func() { _ = c.Shutdown(context.Background()) })
			assert.NotPanics(t, func() { c.Recorder().StepStarted(); logger.Info("after") })
		})
	}
}

func assertWithin(t *testing.T, limit time.Duration, what string, fn func()) {
	t.Helper()
	done := make(chan struct{})
	go func() { defer close(done); fn() }()
	select {
	case <-done:
	case <-time.After(limit):
		t.Fatalf("%s took longer than %s against an unreachable OTLP endpoint", what, limit)
	}
}

func TestNetwork_ShutdownHonoursACallerDeadlineEvenIfTheBackendHangs(t *testing.T) {
	url := blackholeURL(t)
	c := networkController(t, quietLogger())
	cfg := networkConfig(url)
	cfg.ExportTimeout = time.Minute // the caller's deadline must win, not the export timeout
	require.NoError(t, c.Start(cfg))
	c.Recorder().StepStarted()

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = c.Shutdown(ctx)

	assert.Less(t, time.Since(start), 3*time.Second)
}

func TestNetwork_ABackendReturningErrorsIsReportedByFlushOnly(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "overloaded", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	c := networkController(t, quietLogger())
	require.NoError(t, c.Start(networkConfig(srv.URL)))
	c.Recorder().StepStarted()

	err := c.Flush(context.Background())

	assert.Error(t, err)
	assert.NotPanics(t, func() { c.Recorder().StepStarted() }, "recording keeps working while the backend is failing")
}
