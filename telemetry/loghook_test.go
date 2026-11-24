package telemetry

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel/attribute"
	otellog "go.opentelemetry.io/otel/log"
	"go.opentelemetry.io/otel/log/embedded"
	sdklog "go.opentelemetry.io/otel/sdk/log"
)

// exportedLog is the plain-data view of a log record received by the exporter.
type exportedLog struct {
	body     string
	severity otellog.Severity
	ts       time.Time
	attrs    map[string]string
}

type captureExporter struct {
	mu   sync.Mutex
	logs []exportedLog
}

func (c *captureExporter) Export(_ context.Context, records []sdklog.Record) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	for i := range records {
		r := &records[i]
		l := exportedLog{body: r.Body().AsString(), severity: r.Severity(), ts: r.Timestamp(), attrs: map[string]string{}}
		r.WalkAttributes(func(kv attribute.KeyValue) bool {
			l.attrs[string(kv.Key)] = kv.Value.String()
			return true
		})
		c.logs = append(c.logs, l)
	}
	return nil
}
func (*captureExporter) Shutdown(context.Context) error   { return nil }
func (*captureExporter) ForceFlush(context.Context) error { return nil }

func (c *captureExporter) snapshot() []exportedLog {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([]exportedLog(nil), c.logs...)
}

func newHookHarness(t *testing.T, minLevel logrus.Level) (*logrus.Logger, *captureExporter) {
	t.Helper()
	exp := &captureExporter{}
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewSimpleProcessor(exp)))
	t.Cleanup(func() { _ = provider.Shutdown(context.Background()) })

	logger := logrus.New()
	logger.SetOutput(discard{})
	logger.SetLevel(logrus.TraceLevel)
	logger.AddHook(newLogHook(provider.Logger("test"), "vm-7", minLevel))
	return logger, exp
}

type discard struct{}

func (discard) Write(p []byte) (int, error) { return len(p), nil }

func TestLogHook_ShipsMessageSeverityTimestampAndFields(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.InfoLevel)
	when := time.Date(2026, 11, 24, 10, 0, 0, 0, time.UTC)

	logger.WithTime(when).WithFields(logrus.Fields{"step": "build", "attempt": 3, "error": errors.New("503")}).Warn("upload failed")

	logs := exp.snapshot()
	require.Len(t, logs, 1)
	assert.Equal(t, "upload failed", logs[0].body)
	assert.Equal(t, otellog.SeverityWarn, logs[0].severity)
	assert.True(t, when.Equal(logs[0].ts))
	assert.Equal(t, "build", logs[0].attrs["step"])
	assert.Equal(t, "3", logs[0].attrs["attempt"])
	assert.Equal(t, "503", logs[0].attrs["error"], "error fields are shipped as their message")
}

func TestLogHook_MapsEveryLogrusLevel(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.TraceLevel)

	logger.Trace("t")
	logger.Debug("d")
	logger.Info("i")
	logger.Warn("w")
	logger.Error("e")

	var got []otellog.Severity
	for _, l := range exp.snapshot() {
		got = append(got, l.severity)
	}
	assert.Equal(t, []otellog.Severity{
		otellog.SeverityTrace, otellog.SeverityDebug, otellog.SeverityInfo, otellog.SeverityWarn, otellog.SeverityError,
	}, got)
}

func TestLogHook_AlwaysCarriesTheVMIdentityAndFieldsCannotSpoofIt(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.InfoLevel)

	logger.Info("plain")
	logger.WithField("vm_id", "someone-else").Info("spoof attempt")

	logs := exp.snapshot()
	require.Len(t, logs, 2)
	assert.Equal(t, "vm-7", logs[0].attrs["vm_id"])
	assert.Equal(t, "vm-7", logs[1].attrs["vm_id"])
}

func TestLogHook_DropsLevelsBelowTheMinimum(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.InfoLevel)

	logger.Debug("noise")
	logger.Trace("more noise")
	logger.Info("signal")

	logs := exp.snapshot()
	require.Len(t, logs, 1)
	assert.Equal(t, "signal", logs[0].body)
}

// Telemetry's own diagnostics (export failures) are logged through logrus too;
// shipping them would feed failures back into the failing pipe forever.
func TestLogHook_DoesNotShipTelemetryDiagnostics(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.InfoLevel)

	logger.WithField(diagnosticField, true).Warn("otlp export failed")
	logger.Info("real log")

	logs := exp.snapshot()
	require.Len(t, logs, 1)
	assert.Equal(t, "real log", logs[0].body)
}

type panickingLogger struct{ embedded.Logger }

func (panickingLogger) Emit(context.Context, otellog.Record)                    { panic("boom") }
func (panickingLogger) Enabled(context.Context, otellog.EnabledParameters) bool { return true }

func TestLogHook_BackendPanicNeverReachesTheLoggingCaller(t *testing.T) {
	logger := logrus.New()
	logger.SetOutput(discard{})
	logger.AddHook(newLogHook(panickingLogger{}, "vm-7", logrus.InfoLevel))

	require.NotPanics(t, func() { logger.Info("still works") })
}

// blockingExporter simulates an OTLP endpoint that accepts the connection and
// never answers.
type blockingExporter struct{ release chan struct{} }

func (b *blockingExporter) Export(ctx context.Context, _ []sdklog.Record) error {
	select {
	case <-b.release:
	case <-ctx.Done():
	}
	return ctx.Err()
}
func (*blockingExporter) Shutdown(context.Context) error   { return nil }
func (*blockingExporter) ForceFlush(context.Context) error { return nil }

// The unreachable-OTLP guarantee for logs: with the exporter stuck and the
// queue full, logging must stay fast (excess records are dropped, not queued
// behind the caller).
func TestLogHook_StuckExporterNeverBlocksLogging(t *testing.T) {
	exp := &blockingExporter{release: make(chan struct{})}
	defer close(exp.release)
	provider := sdklog.NewLoggerProvider(sdklog.WithProcessor(sdklog.NewBatchProcessor(exp,
		sdklog.WithMaxQueueSize(8), sdklog.WithExportMaxBatchSize(4), sdklog.WithExportTimeout(time.Minute))))
	defer func() {
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	}()
	logger := logrus.New()
	logger.SetOutput(discard{})
	logger.AddHook(newLogHook(provider.Logger("test"), "vm-7", logrus.InfoLevel))

	done := make(chan struct{})
	go func() {
		defer close(done)
		for range 5000 {
			logger.Info("flood")
		}
	}()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("logging blocked behind a stuck OTLP exporter")
	}
}

// Race: many goroutines log concurrently through one hook. Run with -race.
func TestLogHook_ConcurrentLoggingIsRaceFree(t *testing.T) {
	logger, exp := newHookHarness(t, logrus.InfoLevel)

	const workers, per = 8, 100
	var wg sync.WaitGroup
	for w := range workers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range per {
				logger.WithFields(logrus.Fields{"worker": w, "k": "v"}).Info("concurrent")
			}
		}()
	}
	wg.Wait()

	assert.Len(t, exp.snapshot(), workers*per)
}
