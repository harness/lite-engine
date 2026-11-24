package runtime

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logstream/stdout"
	"github.com/harness/lite-engine/telemetry"
)

type observedLogCall struct {
	sub telemetry.Subsystem
	op  telemetry.Operation
}

type logCallSpy struct {
	mu    sync.Mutex
	calls []observedLogCall
}

func (s *logCallSpy) ObserveCall(sub telemetry.Subsystem, op telemetry.Operation, _ time.Duration, _ error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, observedLogCall{sub, op})
}
func (*logCallSpy) StepStarted()  {}
func (*logCallSpy) StepFinished() {}

func (s *logCallSpy) snapshot() []observedLogCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]observedLogCall(nil), s.calls...)
}

func TestGetLogServiceClient_RemoteClientsAreReportedInBothVariants(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()

	for name, optimized := range map[string]bool{"standard": false, "optimized": true} {
		t.Run(name, func(t *testing.T) {
			spy := &logCallSpy{}

			client := getLogServiceClientWithRecorder(&api.LogConfig{URL: srv.URL, AccountID: "acc", Token: "t"}, optimized, spy)
			require.NoError(t, client.Open(context.Background(), "key"))

			assert.Equal(t, []observedLogCall{{telemetry.SubsystemLogService, telemetry.OpLogOpen}}, spy.snapshot())
		})
	}
}

func TestGetLogServiceClient_StdoutFallbackIsNotARemoteCallAndStaysUnwrapped(t *testing.T) {
	client := getLogServiceClientWithRecorder(&api.LogConfig{}, false, &logCallSpy{})

	assert.IsType(t, &stdout.Logger{}, client)
}
