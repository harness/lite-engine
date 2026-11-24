package logstream

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harness/lite-engine/telemetry"
)

type recordedCall struct {
	sub telemetry.Subsystem
	op  telemetry.Operation
	d   time.Duration
	err error
}

type spyRecorder struct {
	mu    sync.Mutex
	calls []recordedCall
}

func (s *spyRecorder) ObserveCall(sub telemetry.Subsystem, op telemetry.Operation, d time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, recordedCall{sub, op, d, err})
}
func (*spyRecorder) StepStarted()  {}
func (*spyRecorder) StepFinished() {}

func (s *spyRecorder) only(t *testing.T) recordedCall {
	t.Helper()
	s.mu.Lock()
	defer s.mu.Unlock()
	require.Len(t, s.calls, 1)
	return s.calls[0]
}

type panickingRecorder struct{}

func (panickingRecorder) ObserveCall(telemetry.Subsystem, telemetry.Operation, time.Duration, error) {
	panic("boom")
}
func (panickingRecorder) StepStarted()  { panic("boom") }
func (panickingRecorder) StepFinished() { panic("boom") }

// stubClient is a Client with a scripted error that remembers its arguments.
type stubClient struct {
	err      error
	delay    time.Duration
	gotKey   string
	gotLines []*Line
	gotSnap  bool
	gotCtx   context.Context
}

func (s *stubClient) record(ctx context.Context, key string) error {
	s.gotCtx, s.gotKey = ctx, key
	time.Sleep(s.delay)
	return s.err
}
func (s *stubClient) Upload(ctx context.Context, key string, lines []*Line) error {
	s.gotLines = lines
	return s.record(ctx, key)
}
func (s *stubClient) Open(ctx context.Context, key string) error { return s.record(ctx, key) }
func (s *stubClient) Close(ctx context.Context, key string, snapshot bool) error {
	s.gotSnap = snapshot
	return s.record(ctx, key)
}
func (s *stubClient) Write(ctx context.Context, key string, lines []*Line) error {
	s.gotLines = lines
	return s.record(ctx, key)
}

// statelessClient succeeds and keeps no state, so it is safe to share across goroutines.
type statelessClient struct{}

func (statelessClient) Upload(context.Context, string, []*Line) error { return nil }
func (statelessClient) Open(context.Context, string) error            { return nil }
func (statelessClient) Close(context.Context, string, bool) error     { return nil }
func (statelessClient) Write(context.Context, string, []*Line) error  { return nil }

type ctxKey struct{}

func TestInstrumentedClient_EachOperationDelegatesAndIsRecordedUnderItsOwnName(t *testing.T) {
	lines := []*Line{{Number: 1, Message: "hi"}}
	boom := errors.New("503")
	tests := []struct {
		name string
		call func(c Client, ctx context.Context) error
		op   telemetry.Operation
	}{
		{"open", func(c Client, ctx context.Context) error { return c.Open(ctx, "key") }, telemetry.OpLogOpen},
		{"write", func(c Client, ctx context.Context) error { return c.Write(ctx, "key", lines) }, telemetry.OpLogWrite},
		{"close", func(c Client, ctx context.Context) error { return c.Close(ctx, "key", true) }, telemetry.OpLogClose},
		{"upload", func(c Client, ctx context.Context) error { return c.Upload(ctx, "key", lines) }, telemetry.OpLogUpload},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			stub := &stubClient{err: boom, delay: 5 * time.Millisecond}
			spy := &spyRecorder{}
			ctx := context.WithValue(context.Background(), ctxKey{}, "marker")

			got := tt.call(NewInstrumentedClient(stub, spy), ctx)

			require.Same(t, boom, got, "the caller sees the wrapped client's exact error")
			assert.Equal(t, "key", stub.gotKey)
			assert.Equal(t, "marker", stub.gotCtx.Value(ctxKey{}), "the caller's context is passed through untouched")
			rec := spy.only(t)
			assert.Equal(t, telemetry.SubsystemLogService, rec.sub)
			assert.Equal(t, tt.op, rec.op)
			assert.Same(t, boom, rec.err)
			assert.GreaterOrEqual(t, rec.d, 5*time.Millisecond)
		})
	}
}

func TestInstrumentedClient_ForwardsArguments(t *testing.T) {
	stub := &stubClient{}
	c := NewInstrumentedClient(stub, &spyRecorder{})
	lines := []*Line{{Number: 7, Message: "payload"}}

	require.NoError(t, c.Write(context.Background(), "k", lines))
	assert.Same(t, lines[0], stub.gotLines[0])

	require.NoError(t, c.Close(context.Background(), "k", true))
	assert.True(t, stub.gotSnap)
}

func TestInstrumentedClient_SuccessIsRecordedWithoutError(t *testing.T) {
	spy := &spyRecorder{}

	require.NoError(t, NewInstrumentedClient(&stubClient{}, spy).Open(context.Background(), "k"))

	assert.NoError(t, spy.only(t).err)
}

func TestInstrumentedClient_APanickingRecorderCannotBreakLogging(t *testing.T) {
	want := errors.New("real failure")
	c := NewInstrumentedClient(&stubClient{err: want}, panickingRecorder{})

	var got error
	require.NotPanics(t, func() { got = c.Write(context.Background(), "k", nil) })

	assert.Same(t, want, got)
}

func TestNewInstrumentedClient_WithoutARecorderReturnsTheClientItself(t *testing.T) {
	stub := &stubClient{}

	assert.Same(t, stub, NewInstrumentedClient(stub, nil))
}

// Race: one wrapped client is shared by every log stream of the stage.
func TestInstrumentedClient_ConcurrentUseIsRaceFree(t *testing.T) {
	spy := &spyRecorder{}
	c := NewInstrumentedClient(statelessClient{}, spy)

	var wg sync.WaitGroup
	for range 8 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for range 100 {
				_ = c.Open(context.Background(), "k")
			}
		}()
	}
	wg.Wait()

	assert.Len(t, spy.calls, 800)
}
