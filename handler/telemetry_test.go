package handler

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/telemetry"
)

type fakeStarter struct {
	calls []telemetry.Config
	err   error
	panic bool
}

func (f *fakeStarter) Start(cfg telemetry.Config) error {
	f.calls = append(f.calls, cfg)
	if f.panic {
		panic("boom")
	}
	return f.err
}

func quietEntry() *logrus.Entry {
	l := logrus.New()
	l.SetLevel(logrus.PanicLevel)
	return logrus.NewEntry(l)
}

func TestStartTelemetry_AbsentConfigStartsNothing(t *testing.T) {
	starter := &fakeStarter{}

	startTelemetry(starter, nil, quietEntry())

	assert.Empty(t, starter.calls, "setup requests from runners that know nothing about telemetry are unaffected")
}

func TestStartTelemetry_MapsTheSetupRequestFieldsOntoTheTelemetryConfig(t *testing.T) {
	starter := &fakeStarter{}
	req := &api.TelemetryConfig{
		Enabled:      true,
		Endpoint:     "http://172.22.64.10:4318",
		MetricsVMID:  true,
		MetricLabels: map[string]string{"pool": "linux-amd64", "region": "us-west1"},
	}

	startTelemetry(starter, req, quietEntry())

	require.Len(t, starter.calls, 1)
	assert.Equal(t, telemetry.Config{
		Enabled:      true,
		Endpoint:     "http://172.22.64.10:4318",
		MetricsVMID:  true,
		MetricLabels: map[string]string{"pool": "linux-amd64", "region": "us-west1"},
	}, starter.calls[0])
}

// The core fail-open rule at the API boundary: a telemetry problem must never
// become a setup failure.
func TestStartTelemetry_FailuresAndPanicsNeverEscapeSetup(t *testing.T) {
	tests := map[string]*fakeStarter{
		"start returns an error": {err: errors.New("bad endpoint")},
		"start panics":           {panic: true},
	}
	for name, starter := range tests {
		t.Run(name, func(t *testing.T) {
			require.NotPanics(t, func() {
				startTelemetry(starter, &api.TelemetryConfig{Enabled: true, Endpoint: "http://h:1"}, quietEntry())
			})
		})
	}
}

func TestSetupRequestJSON_TelemetryIsOptionalAndDecodesFromTheWireFormat(t *testing.T) {
	var without api.SetupRequest
	require.NoError(t, json.Unmarshal([]byte(`{"network":{}}`), &without))
	assert.Nil(t, without.Telemetry)

	var with api.SetupRequest
	require.NoError(t, json.Unmarshal([]byte(
		`{"telemetry":{"enabled":true,"endpoint":"http://gw:4318","metrics_vm_id":true,"metric_labels":{"pool":"p"}}}`), &with))
	require.NotNil(t, with.Telemetry)
	assert.Equal(t, api.TelemetryConfig{
		Enabled: true, Endpoint: "http://gw:4318", MetricsVMID: true, MetricLabels: map[string]string{"pool": "p"},
	}, *with.Telemetry)
}

type fakeFlusher struct {
	err     error
	block   bool
	flushed int
}

func (f *fakeFlusher) Flush(ctx context.Context) error {
	f.flushed++
	if f.block {
		<-ctx.Done()
		return ctx.Err()
	}
	return f.err
}

func TestFlushTelemetry_DeliversBufferedDataBeforeDestroyReturns(t *testing.T) {
	f := &fakeFlusher{}

	flushTelemetry(context.Background(), f, time.Second, quietEntry())

	assert.Equal(t, 1, f.flushed)
}

func TestFlushTelemetry_ErrorsAreSwallowed(t *testing.T) {
	f := &fakeFlusher{err: errors.New("backend down")}

	require.NotPanics(t, func() { flushTelemetry(context.Background(), f, time.Second, quietEntry()) })
}

// Destroy must not hang on a dead backend: the VM is about to be discarded.
func TestFlushTelemetry_IsBoundedWhenTheBackendHangs(t *testing.T) {
	f := &fakeFlusher{block: true}

	start := time.Now()
	flushTelemetry(context.Background(), f, 100*time.Millisecond, quietEntry())

	assert.Less(t, time.Since(start), 2*time.Second)
}
