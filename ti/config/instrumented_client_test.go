package config

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	v2types "github.com/harness/ti-client/chrysalis/types"
	"github.com/harness/ti-client/client"
	"github.com/harness/ti-client/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/harness/lite-engine/telemetry"
)

type tiCall struct {
	sub telemetry.Subsystem
	op  telemetry.Operation
	err error
}

type tiSpy struct {
	mu    sync.Mutex
	calls []tiCall
}

func (s *tiSpy) ObserveCall(sub telemetry.Subsystem, op telemetry.Operation, _ time.Duration, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls = append(s.calls, tiCall{sub, op, err})
}
func (*tiSpy) StepStarted()  {}
func (*tiSpy) StepFinished() {}

func (s *tiSpy) snapshot() []tiCall {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]tiCall(nil), s.calls...)
}

// scriptedTI implements only the methods the tests touch; the embedded nil
// interface makes any other call fail loudly instead of silently passing.
type scriptedTI struct {
	client.Client
	err error
}

func (s scriptedTI) Write(context.Context, string, string, []*types.TestCase) error { return s.err }
func (s scriptedTI) UploadCg(context.Context, string, string, string, int64, []byte, bool) error {
	return s.err
}
func (s scriptedTI) UploadCgV2(context.Context, v2types.UploadCgRequest, string, int64, string, string) error {
	return s.err
}
func (s scriptedTI) CommitInfo(context.Context, string, string) (types.CommitInfoResp, error) {
	return types.CommitInfoResp{LastSuccessfulCommitId: "abc"}, s.err
}

func TestInstrumentTIClient_ReportTestsAndCallgraphUploadsAreRecordedWithTheirErrors(t *testing.T) {
	boom := errors.New("ti 500")
	tests := []struct {
		name string
		call func(c client.Client) error
		op   telemetry.Operation
	}{
		{"report", func(c client.Client) error { return c.Write(context.Background(), "step", "junit", nil) }, telemetry.OpTIReport},
		{"callgraph v1", func(c client.Client) error {
			return c.UploadCg(context.Background(), "step", "src", "tgt", 1, nil, false)
		}, telemetry.OpTIUploadCallgraph},
		{"callgraph v2", func(c client.Client) error {
			return c.UploadCgV2(context.Background(), v2types.UploadCgRequest{}, "step", 1, "src", "tgt")
		}, telemetry.OpTIUploadCallgraph},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			spy := &tiSpy{}

			got := tt.call(instrumentTIClient(scriptedTI{err: boom}, spy))

			require.Same(t, boom, got, "the caller sees the TI client's exact error")
			assert.Equal(t, []tiCall{{telemetry.SubsystemTIService, tt.op, boom}}, spy.snapshot())
		})
	}
}

func TestInstrumentTIClient_SuccessIsRecordedWithoutError(t *testing.T) {
	spy := &tiSpy{}

	require.NoError(t, instrumentTIClient(scriptedTI{}, spy).Write(context.Background(), "s", "junit", nil))

	assert.Equal(t, []tiCall{{telemetry.SubsystemTIService, telemetry.OpTIReport, nil}}, spy.snapshot())
}

// Only the calls the monitoring plan names are observed; everything else must
// pass through to the real client with identical results.
func TestInstrumentTIClient_OtherMethodsAreDelegatedAndNotRecorded(t *testing.T) {
	spy := &tiSpy{}

	info, err := instrumentTIClient(scriptedTI{}, spy).CommitInfo(context.Background(), "step", "main")

	require.NoError(t, err)
	assert.Equal(t, "abc", info.LastSuccessfulCommitId)
	assert.Empty(t, spy.snapshot())
}

type panickyRecorder struct{}

func (panickyRecorder) ObserveCall(telemetry.Subsystem, telemetry.Operation, time.Duration, error) {
	panic("boom")
}
func (panickyRecorder) StepStarted()  {}
func (panickyRecorder) StepFinished() {}

func TestInstrumentTIClient_APanickingRecorderCannotBreakTI(t *testing.T) {
	want := errors.New("real failure")

	var got error
	require.NotPanics(t, func() {
		got = instrumentTIClient(scriptedTI{err: want}, panickyRecorder{}).Write(context.Background(), "s", "junit", nil)
	})

	assert.Same(t, want, got)
}

func TestCfgGetClient_ReturnsNilWhenThereIsNoClient(t *testing.T) {
	var cfg Cfg

	assert.Nil(t, cfg.GetClient(), "callers rely on a nil client for an unconfigured Cfg")
}

func TestCfgGetClient_ReturnsAUsableInstrumentedClient(t *testing.T) {
	cfg := New("http://ti.invalid", "tok", "acc", "org", "proj", "pipe", "build", "stage", "repo", "sha", "", "", "", "", "/tmp", "", false, "", "")

	c := cfg.GetClient()

	require.NotNil(t, c)
	assert.Equal(t, "repo", cfg.GetRepo(), "identity accessors keep reading the underlying client")
	assert.Equal(t, "sha", cfg.GetSha())
}
