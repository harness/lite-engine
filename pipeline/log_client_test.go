package pipeline

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
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/logstream/filestore"
	"github.com/harness/lite-engine/telemetry"
)

type spyCall struct {
	sub telemetry.Subsystem
	op  telemetry.Operation
	err error
}

type spyRecorder struct {
	mu    sync.Mutex
	calls []spyCall
}

func (s *spyRecorder) ObserveCall(sub telemetry.Subsystem, op telemetry.Operation, _ time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, spyCall{sub, op, err})
}
func (*spyRecorder) StepStarted()  {}
func (*spyRecorder) StepFinished() {}

func (s *spyRecorder) snapshot() []spyCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]spyCall(nil), s.calls...)
}

func TestNewLogStreamClient_RemoteLogServiceCallsAreReported(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) }))
	defer srv.Close()
	spy := &spyRecorder{}

	client := newLogStreamClient(&api.LogConfig{URL: srv.URL, AccountID: "acc", Token: "t"}, &spec.MtlsConfig{}, spy)
	require.NoError(t, client.Open(context.Background(), "key"))

	calls := spy.snapshot()
	require.Len(t, calls, 1)
	assert.Equal(t, spyCall{telemetry.SubsystemLogService, telemetry.OpLogOpen, nil}, calls[0])
}

func TestNewLogStreamClient_LocalFileStoreIsNotARemoteCallAndStaysUnwrapped(t *testing.T) {
	client := newLogStreamClient(&api.LogConfig{}, &spec.MtlsConfig{}, &spyRecorder{})

	assert.IsType(t, &filestore.FileStore{}, client)
}
