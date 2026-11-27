// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/errors"
	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/osstats"
	"github.com/harness/lite-engine/pipeline"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockLogWriter struct {
	closeErr error
	errVal   error
}

func (m *mockLogWriter) Write(p []byte) (int, error) { return len(p), nil }
func (m *mockLogWriter) Open() error                 { return nil }
func (m *mockLogWriter) Start()                      {}
func (m *mockLogWriter) Close() error                { return m.closeErr }
func (m *mockLogWriter) Error() error                { return m.errVal }

func newTestStepExecutor() *StepExecutor {
	return &StepExecutor{
		mu:         sync.Mutex{},
		stepStatus: make(map[string]StepStatus),
		stepLog:    make(map[string]*StepLog),
		stepWaitCh: make(map[string][]chan StepStatus),
	}
}

func TestStartStepWithStatusUpdate_RejectsEmptyID(t *testing.T) {
	e := newTestStepExecutor()
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: ""})
	require.Error(t, err)
	var br *errors.BadRequestError
	assert.ErrorAs(t, err, &br)
}

func TestStartStepWithStatusUpdate_DuplicateRequestIsIgnored(t *testing.T) {
	e := newTestStepExecutor()

	// Simulate a step that's already in flight by seeding stepStatus.
	// This is exactly what the first call sets before spawning its goroutine.
	const stepID = "step-dup-1"
	e.stepStatus[stepID] = StepStatus{Status: Running}

	// A second call with the same ID must be a no-op: return nil without
	// spawning anything and without overwriting the existing status.
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: stepID})
	require.NoError(t, err)

	e.mu.Lock()
	defer e.mu.Unlock()
	got, ok := e.stepStatus[stepID]
	require.True(t, ok)
	assert.Equal(t, Running, got.Status,
		"duplicate request must not overwrite existing status entry")
	assert.Len(t, e.stepStatus, 1, "duplicate request must not add new map entries")
}

func TestStartStepWithStatusUpdate_ConcurrentDuplicatesAreIgnored(t *testing.T) {
	e := newTestStepExecutor()
	const stepID = "step-concurrent"

	// Pre-seed the map to simulate "step already running" — this is what the
	// first caller's idempotency branch does before spawning its goroutine.
	// All concurrent callers below must hit the duplicate branch and bail out
	// without touching the map. (We seed up-front to avoid letting any of
	// these test calls actually spawn a vm_task_executor goroutine, which
	// would then try to run a step with a nil engine.)
	e.stepStatus[stepID] = StepStatus{Status: Running}

	const concurrent = 50
	var wg sync.WaitGroup
	wg.Add(concurrent)
	for i := 0; i < concurrent; i++ {
		go func() {
			defer wg.Done()
			err := e.StartStepWithStatusUpdate(context.Background(),
				&api.StartStepRequest{ID: stepID})
			assert.NoError(t, err)
		}()
	}
	wg.Wait()

	// Exactly one entry, untouched by duplicates.
	e.mu.Lock()
	defer e.mu.Unlock()
	got, ok := e.stepStatus[stepID]
	require.True(t, ok)
	assert.Equal(t, Running, got.Status)
	assert.Len(t, e.stepStatus, 1, "concurrent duplicate calls must not add map entries")
}

func TestStartStepWithStatusUpdate_DifferentIDsCoexist(t *testing.T) {
	e := newTestStepExecutor()

	// Pre-populate two different step IDs as if both are running.
	e.stepStatus["step-a"] = StepStatus{Status: Running}
	e.stepStatus["step-b"] = StepStatus{Status: Running}

	// A duplicate of one ID must not affect the other.
	err := e.StartStepWithStatusUpdate(context.Background(), &api.StartStepRequest{ID: "step-a"})
	require.NoError(t, err)

	e.mu.Lock()
	defer e.mu.Unlock()
	assert.Len(t, e.stepStatus, 2)
	assert.Equal(t, Running, e.stepStatus["step-a"].Status)
	assert.Equal(t, Running, e.stepStatus["step-b"].Status)
}

func newTestTiConfig() *tiCfg.Cfg {
	cfg := tiCfg.New("", "", "", "", "", "", "", "", "", "", "", "", "", "", "", "", false, "", "")
	return &cfg
}

// runStepHelper calls executeStepHelper and returns only the step state and the
// error, which are the two values the log resilience tests assert on.
func runStepHelper(ctx context.Context, r *api.StartStepRequest, f RunFunc, wr logstream.Writer,
	logServiceResilience bool) (*runtime.State, error) {
	state, _, _, _, _, _, _, err := executeStepHelper(ctx, r, f, wr, newTestTiConfig(), false, logServiceResilience) //nolint:dogsled
	return state, err
}

func TestExecuteStepHelper_CloseErrorIgnoredOnSuccess(t *testing.T) {
	ctx := context.Background()
	r := &api.StartStepRequest{
		ID:     "step-close-pass",
		Name:   "test-step",
		LogKey: "log-key-1",
		Kind:   api.Run,
		Run:    api.RunConfig{Command: []string{"echo", "hello"}},
		Envs:   map[string]string{},
	}

	mockWr := &mockLogWriter{
		closeErr: fmt.Errorf("log service unavailable"),
	}

	runFn := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return &runtime.State{Exited: true, ExitCode: 0}, nil
	}

	exited, err := runStepHelper(ctx, r, runFn, mockWr, true)
	assert.NoError(t, err, "log close error should not fail a passing step when flag is enabled")
	assert.NotNil(t, exited)
	assert.Equal(t, 0, exited.ExitCode)
}

func TestExecuteStepHelper_CloseErrorPropagatedWhenFlagDisabled(t *testing.T) {
	ctx := context.Background()
	r := &api.StartStepRequest{
		ID:     "step-close-no-flag",
		Name:   "test-step-no-flag",
		LogKey: "log-key-4",
		Kind:   api.Run,
		Run:    api.RunConfig{Command: []string{"echo", "hello"}},
		Envs:   map[string]string{},
	}

	mockWr := &mockLogWriter{
		closeErr: fmt.Errorf("log service unavailable"),
	}

	runFn := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return &runtime.State{Exited: true, ExitCode: 0}, nil
	}

	_, err := runStepHelper(ctx, r, runFn, mockWr, false)
	assert.Error(t, err, "log close error should propagate when flag is not set")
	assert.Contains(t, err.Error(), "log service unavailable")
}

func TestExecuteStepHelper_CloseErrorPropagatedWhenStateUnknown(t *testing.T) {
	ctx := context.Background()
	r := &api.StartStepRequest{
		ID:     "step-close-nil-state",
		Name:   "test-step-nil-state",
		LogKey: "log-key-5",
		Kind:   api.Run,
		Run:    api.RunConfig{Command: []string{"echo", "hello"}},
		Envs:   map[string]string{},
	}

	mockWr := &mockLogWriter{
		closeErr: fmt.Errorf("log service unavailable"),
	}

	// A nil state with no error means the execution result is unknown, so it must
	// not be treated as a pass even when the resilience flag is enabled.
	runFn := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return nil, nil
	}

	exited, err := runStepHelper(ctx, r, runFn, mockWr, true)
	assert.Nil(t, exited)
	assert.Error(t, err, "log close error should propagate when the step state is unknown")
	assert.Contains(t, err.Error(), "log service unavailable")
}

func TestExecuteStepHelper_CloseErrorAppendedOnFailure(t *testing.T) {
	ctx := context.Background()
	r := &api.StartStepRequest{
		ID:     "step-close-fail",
		Name:   "test-step-fail",
		LogKey: "log-key-2",
		Kind:   api.Run,
		Run:    api.RunConfig{Command: []string{"false"}},
		Envs:   map[string]string{},
	}

	mockWr := &mockLogWriter{
		closeErr: fmt.Errorf("log service unavailable"),
	}

	runFn := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return &runtime.State{Exited: true, ExitCode: 1}, nil
	}

	_, err := runStepHelper(ctx, r, runFn, mockWr, true)
	assert.Error(t, err, "log close error should be included when step already failed")
	assert.Contains(t, err.Error(), "log service unavailable")
}

// On a non-zero exit, a writer error only gates the append — the value appended
// is the run error, not the writer error itself. The log resilience change left
// that behavior untouched, so the nudge text never reaches the step result.
func TestExecuteStepHelper_WriterErrorAppendsRunError(t *testing.T) {
	ctx := context.Background()
	r := &api.StartStepRequest{
		ID:     "step-wr-err",
		Name:   "test-step-wr-err",
		LogKey: "log-key-3",
		Kind:   api.Run,
		Run:    api.RunConfig{Command: []string{"false"}},
		Envs:   map[string]string{},
	}

	mockWr := &mockLogWriter{
		errVal: fmt.Errorf("nudge: possible error on line 42"),
	}

	runFn := func(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool, isHosted bool) (*runtime.State, error) {
		return &runtime.State{Exited: true, ExitCode: 1}, fmt.Errorf("command exited with code 1")
	}

	_, err := runStepHelper(ctx, r, runFn, mockWr, false)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "command exited with code 1")
	assert.NotContains(t, err.Error(), "nudge: possible error on line 42")
}

func TestReadNativeArtifactURLUsesVersionAndProcessEnv(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HARNESS_WORKDIR", dir)
	shared := filepath.Join(dir, "tmp", "engine")
	require.NoError(t, os.MkdirAll(shared, 0o755))
	payload := `{"artifactName":"first-k8","version":"v409","registry":"harness-native-registry-abc","fileCount":1,"totalBytes":10}`
	require.NoError(t, os.WriteFile(filepath.Join(shared, "step1-artifact.json"), []byte(payload), 0o600))

	// Account id is a stage env var, not always present on the step request map.
	t.Setenv("HARNESS_ACCOUNT_ID", "acc")
	envs := map[string]string{
		"HARNESS_ORG_ID":           "org",
		"HARNESS_PROJECT_ID":       "proj",
		"HARNESS_HAR_URL":          "https://pkg.harness.io/",
		"HARNESS_REGISTRY_API_URL": "https://app.harness.io/",
	}
	_, vars := readNativeArtifact("step1", envs)
	require.NotNil(t, vars)
	assert.Equal(t,
		"https://app.harness.io/ng/account/acc/module/har/orgs/org/projects/proj/registries/harness-native-registry-abc/artifacts/first-k8/versions/v409/artifact_details",
		vars["ARTIFACT_URLS"])
}

const sampleGitCloneReceipt = `{"repoUrl":"https://github.com/acme/repo.git","commitSha":"abc123","linesOfCode":42}`

func gitCloneReceiptPath(stepID string) string {
	return fmt.Sprintf("%s/%s-git-clone-metadata.json", pipeline.GetSharedVolPath(), stepID)
}

func writeGitCloneReceipt(t *testing.T, stepID, content string) {
	t.Helper()
	path := gitCloneReceiptPath(stepID)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestReadGitCloneMetadata_FilePresent_ReturnsRawJSON(t *testing.T) {
	writeGitCloneReceipt(t, "gc-present", sampleGitCloneReceipt)
	assert.Equal(t, sampleGitCloneReceipt, readGitCloneMetadata("gc-present"))
}

func TestReadGitCloneMetadata_FileMissing_ReturnsEmpty(t *testing.T) {
	assert.Empty(t, readGitCloneMetadata("gc-does-not-exist"))
}

func TestReadGitCloneMetadata_EmptyFile_ReturnsEmpty(t *testing.T) {
	writeGitCloneReceipt(t, "gc-empty", "")
	assert.Empty(t, readGitCloneMetadata("gc-empty"))
}

func TestReadGitCloneMetadata_IsolatedPerStep(t *testing.T) {
	writeGitCloneReceipt(t, "gc-a", `{"commitSha":"aaa"}`)
	writeGitCloneReceipt(t, "gc-b", `{"commitSha":"bbb"}`)
	a := readGitCloneMetadata("gc-a")
	b := readGitCloneMetadata("gc-b")
	assert.Contains(t, a, "aaa")
	assert.NotContains(t, a, "bbb")
	assert.Contains(t, b, "bbb")
	assert.NotContains(t, b, "aaa")
}

func TestReadGitCloneMetadata_DoesNotParse(t *testing.T) {
	writeGitCloneReceipt(t, "gc-raw", "not json at all")
	assert.Equal(t, "not json at all", readGitCloneMetadata("gc-raw"))
}

func TestReadGitCloneMetadata_IgnoresEnvPathOutsideSharedVol(t *testing.T) {
	custom := filepath.Join(t.TempDir(), "custom-receipt.json")
	require.NoError(t, os.WriteFile(custom, []byte(sampleGitCloneReceipt), 0o644))
	assert.Empty(t, readGitCloneMetadata("gc-custom"))
}

func TestReadGitCloneMetadata_ReadsEngineOwnedPathOnly(t *testing.T) {
	writeGitCloneReceipt(t, "gc-owned", sampleGitCloneReceipt)
	assert.Equal(t, sampleGitCloneReceipt, readGitCloneMetadata("gc-owned"))
}

func TestReadGitCloneMetadata_RejectsSymlink(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	secret := filepath.Join(t.TempDir(), "host-secret")
	require.NoError(t, os.WriteFile(secret, []byte("host-secret-bytes"), 0o644))
	link := gitCloneReceiptPath("gc-symlink")
	if err := os.Symlink(secret, link); err != nil {
		t.Skipf("symlinks not permitted: %v", err)
	}
	t.Cleanup(func() { _ = os.Remove(link) })
	assert.Empty(t, readGitCloneMetadata("gc-symlink"))
}

func TestReadGitCloneMetadata_RejectsStepIDPathEscape(t *testing.T) {
	initPipelineState(t)
	shared := pipeline.GetSharedVolPath()
	require.NoError(t, os.MkdirAll(shared, 0o755))
	escaped := filepath.Clean(filepath.Join(shared, "..", "escape-git-clone-metadata.json"))
	require.NoError(t, os.WriteFile(escaped, []byte("should-not-be-read"), 0o644))
	t.Cleanup(func() { _ = os.Remove(escaped) })
	assert.Empty(t, readGitCloneMetadata("../escape"))
}

func TestReadGitCloneMetadata_RejectsOversizedFile(t *testing.T) {
	huge := strings.Repeat("x", maxGitCloneMetadataBytes+1)
	writeGitCloneReceipt(t, "gc-huge", huge)
	assert.Empty(t, readGitCloneMetadata("gc-huge"))
	_, err := os.Stat(gitCloneReceiptPath("gc-huge"))
	assert.True(t, os.IsNotExist(err), "oversized receipt must be removed")
}

func TestReadGitCloneMetadata_RejectsDirectory(t *testing.T) {
	path := gitCloneReceiptPath("gc-dir")
	require.NoError(t, os.MkdirAll(path, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(path) })
	assert.Empty(t, readGitCloneMetadata("gc-dir"))
}

func TestReadGitCloneMetadata_RemovesFileAfterRead(t *testing.T) {
	writeGitCloneReceipt(t, "gc-rm", sampleGitCloneReceipt)
	assert.Equal(t, sampleGitCloneReceipt, readGitCloneMetadata("gc-rm"))
	_, err := os.Stat(gitCloneReceiptPath("gc-rm"))
	assert.True(t, os.IsNotExist(err))
}

func TestConvertStatus_CarriesGitCloneMetadata(t *testing.T) {
	resp := convertStatus(StepStatus{
		Status:           Complete,
		State:            &runtime.State{Exited: true, ExitCode: 0},
		GitCloneMetadata: sampleGitCloneReceipt,
	})
	assert.Equal(t, sampleGitCloneReceipt, resp.GitCloneMetadata)
}

func TestConvertStatus_NoMetadata_LeavesFieldEmpty(t *testing.T) {
	resp := convertStatus(StepStatus{Status: Complete, State: &runtime.State{Exited: true}})
	assert.Empty(t, resp.GitCloneMetadata)
}

func TestConvertPollResponse_SuccessCarriesMetadata(t *testing.T) {
	poll := &api.PollStepResponse{GitCloneMetadata: sampleGitCloneReceipt}
	vm := convertPollResponse(poll, nil)
	assert.Equal(t, api.Success, vm.CommandExecutionStatus)
	assert.Equal(t, sampleGitCloneReceipt, vm.GitCloneMetadata)
}

func TestConvertPollResponse_FailureCarriesMetadata(t *testing.T) {
	// nil envs -> TestSummaryAsOutputEnabled false -> plain-failure branch
	poll := &api.PollStepResponse{Error: "boom", GitCloneMetadata: sampleGitCloneReceipt}
	vm := convertPollResponse(poll, nil)
	assert.Equal(t, api.Failure, vm.CommandExecutionStatus)
	assert.Equal(t, sampleGitCloneReceipt, vm.GitCloneMetadata)
}

func TestPollStepResponse_JSONKey_GitCloneMetadata(t *testing.T) {
	with, err := json.Marshal(&api.PollStepResponse{GitCloneMetadata: sampleGitCloneReceipt})
	require.NoError(t, err)
	assert.Contains(t, string(with), `"git_clone_metadata"`)
	assert.NotContains(t, string(with), "explicit_git_clone_metadata")

	without, err := json.Marshal(&api.PollStepResponse{})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "git_clone_metadata")
}

func TestVMTaskExecutionResponse_JSONKey_GitCloneMetadata(t *testing.T) {
	// The delegate's VmTaskExecutionResponse bean binds to exactly this wire key.
	with, err := json.Marshal(api.VMTaskExecutionResponse{GitCloneMetadata: sampleGitCloneReceipt})
	require.NoError(t, err)
	assert.Contains(t, string(with), `"git_clone_metadata"`)
	assert.NotContains(t, string(with), "explicit_git_clone_metadata")

	without, err := json.Marshal(api.VMTaskExecutionResponse{})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "git_clone_metadata")
}

func TestConvertPollResponse_FailureWithTestSummaryCarriesMetadata(t *testing.T) {
	envs := map[string]string{"HARNESS_CI_TEST_SUMMARY_OUTPUT_FF": "true"}
	poll := &api.PollStepResponse{Error: "boom", GitCloneMetadata: sampleGitCloneReceipt}
	vm := convertPollResponse(poll, envs)
	assert.Equal(t, api.Failure, vm.CommandExecutionStatus)
	assert.Equal(t, sampleGitCloneReceipt, vm.GitCloneMetadata)
}

// initPipelineState seeds the global pipeline singleton with a valid (empty)
// TI config. Without it, the singleton's zero-value tiCfg.Cfg has a nil inner
// client and the stateful executeStep path panics in GetURL.
func initPipelineState(t *testing.T) {
	t.Helper()
	pipeline.GetState().Set(nil, api.LogConfig{}, *newTestTiConfig(), spec.MtlsConfig{}, &osstats.StatsCollector{})
}

// hostEngine returns an engine with docker setup disabled: steps with no image
// run directly on the host via exec.Run, which is all these tests need.
func hostEngine(t *testing.T) *engine.Engine {
	t.Helper()
	disableDocker := false
	eng := &engine.Engine{}
	require.NoError(t, eng.Setup(context.Background(),
		&spec.PipelineConfig{EnableDockerSetup: &disableDocker, Envs: map[string]string{}}))
	return eng
}

// startAndPoll drives a full stateful StartStep → PollStep cycle on a host engine.
func startAndPoll(t *testing.T, req *api.StartStepRequest) *api.PollStepResponse {
	t.Helper()
	exec := NewStepExecutor(hostEngine(t))
	require.NoError(t, exec.StartStep(context.Background(), req))

	done := make(chan struct{})
	var resp *api.PollStepResponse
	var err error
	go func() {
		resp, err = exec.PollStep(context.Background(), &api.PollStepRequest{ID: req.ID})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(20 * time.Second):
		t.Fatal("PollStep timed out")
	}
	require.NoError(t, err)
	require.NotNil(t, resp)
	return resp
}

func gitCloneWriterStepRequest(t *testing.T, id string, envs map[string]string) *api.StartStepRequest {
	return &api.StartStepRequest{
		ID: id, Name: "clone", Kind: api.Run, LogKey: id,
		WorkingDir: t.TempDir(),
		Envs:       envs,
		Run: api.RunConfig{
			Entrypoint: []string{"/bin/sh", "-c"},
			Command:    []string{`printf %s "$RECEIPT" > "$HARNESS_GIT_CLONE_METADATA_FILE"`},
		},
	}
}

func TestStartStep_PollCarriesGitCloneMetadataWrittenToEnvPath(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-start-present"
	t.Cleanup(func() { _ = os.Remove(gitCloneReceiptPath(id)) })

	req := gitCloneWriterStepRequest(t, id, map[string]string{"RECEIPT": sampleGitCloneReceipt})
	resp := startAndPoll(t, req)
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, sampleGitCloneReceipt, resp.GitCloneMetadata)
	_, err := os.Stat(gitCloneReceiptPath(id))
	assert.True(t, os.IsNotExist(err), "receipt must be removed after collection")
}

func TestStartStep_SecondPollKeepsGitCloneMetadataAfterFileRemoved(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-poll-twice"
	t.Cleanup(func() { _ = os.Remove(gitCloneReceiptPath(id)) })

	req := gitCloneWriterStepRequest(t, id, map[string]string{"RECEIPT": sampleGitCloneReceipt})
	exec := NewStepExecutor(hostEngine(t))
	require.NoError(t, exec.StartStep(context.Background(), req))
	first, err := exec.PollStep(context.Background(), &api.PollStepRequest{ID: id})
	require.NoError(t, err)
	assert.Equal(t, sampleGitCloneReceipt, first.GitCloneMetadata)
	_, statErr := os.Stat(gitCloneReceiptPath(id))
	assert.True(t, os.IsNotExist(statErr))

	second, err := exec.PollStep(context.Background(), &api.PollStepRequest{ID: id})
	require.NoError(t, err)
	assert.Equal(t, sampleGitCloneReceipt, second.GitCloneMetadata)
}

func TestStartStep_FreshExecutorReusedStepIDDoesNotReturnStaleReceipt(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-reuse-id"
	writeGitCloneReceipt(t, id, sampleGitCloneReceipt)

	noWrite := &api.StartStepRequest{
		ID: id, Name: "clone", Kind: api.Run, LogKey: id,
		WorkingDir: t.TempDir(),
		Envs:       map[string]string{},
		Run: api.RunConfig{
			Entrypoint: []string{"/bin/sh", "-c"},
			Command:    []string{"true"},
		},
	}

	first := startAndPoll(t, noWrite)
	assert.Equal(t, 0, first.ExitCode)
	assert.Empty(t, first.GitCloneMetadata, "stale disk receipt must not be returned")

	writeGitCloneReceipt(t, id, `{"commitSha":"stale-again"}`)
	second := startAndPoll(t, noWrite)
	assert.Equal(t, 0, second.ExitCode)
	assert.Empty(t, second.GitCloneMetadata, "fresh executor must not pick up a leftover receipt")
}

func TestStartStep_PollLeavesGitCloneMetadataEmptyWhenFileMissing(t *testing.T) {
	initPipelineState(t)
	id := "gc-start-missing"
	req := &api.StartStepRequest{
		ID: id, Name: "clone", Kind: api.Run, LogKey: id,
		WorkingDir: t.TempDir(),
		Envs:       map[string]string{},
		Run: api.RunConfig{
			Entrypoint: []string{"/bin/sh", "-c"},
			Command:    []string{"true"},
		},
	}

	resp := startAndPoll(t, req)
	assert.Equal(t, 0, resp.ExitCode)
	assert.Empty(t, resp.GitCloneMetadata)
}

func TestStartStep_PollIgnoresPresetEnvPathAndReadsEngineOwnedFile(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-start-custom"
	t.Cleanup(func() { _ = os.Remove(gitCloneReceiptPath(id)) })
	custom := filepath.Join(t.TempDir(), "receipt.json")
	require.NoError(t, os.WriteFile(custom, []byte(`{"commitSha":"from-preset-env"}`), 0o644))

	req := gitCloneWriterStepRequest(t, id, map[string]string{
		"RECEIPT":                         sampleGitCloneReceipt,
		"HARNESS_GIT_CLONE_METADATA_FILE": custom,
	})
	resp := startAndPoll(t, req)
	assert.Equal(t, 0, resp.ExitCode)
	assert.Equal(t, sampleGitCloneReceipt, resp.GitCloneMetadata)
	preset, err := os.ReadFile(custom)
	require.NoError(t, err)
	assert.Contains(t, string(preset), "from-preset-env", "producer must not write to the preset host path")
}

// StartStepWithStatusUpdate pushes the final VMTaskExecutionResponse to the
// delegate endpoint; the git clone receipt must ride that payload.
func TestStartStepWithStatusUpdate_SendsGitCloneMetadata(t *testing.T) {
	initPipelineState(t)
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-status-update"
	t.Cleanup(func() { _ = os.Remove(gitCloneReceiptPath(id)) })

	bodyCh := make(chan []byte, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodyCh <- b
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	req := gitCloneWriterStepRequest(t, id, map[string]string{"RECEIPT": sampleGitCloneReceipt})
	req.StepStatus = api.StepStatusConfig{
		Endpoint:   srv.URL,
		AccountID:  "acc",
		Token:      "tok",
		DelegateID: "del-1",
		TaskID:     "task-1",
	}

	exec := NewStepExecutor(hostEngine(t))
	require.NoError(t, exec.StartStepWithStatusUpdate(context.Background(), req))

	select {
	case b := <-bodyCh:
		assert.Contains(t, string(b), "git_clone_metadata")
		assert.Contains(t, string(b), "abc123")
	case <-time.After(30 * time.Second):
		t.Fatal("timed out waiting for the step-status POST")
	}
}

// Stateless Run (runner local-exec path) builds the response synchronously;
// the receipt must land on the returned VMTaskExecutionResponse.
func TestStepExecutorStateless_RunCarriesGitCloneMetadata(t *testing.T) {
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))
	id := "gc-stateless"
	t.Cleanup(func() { _ = os.Remove(gitCloneReceiptPath(id)) })

	disableDocker := false
	cfg := &spec.PipelineConfig{EnableDockerSetup: &disableDocker, Envs: map[string]string{}}
	req := gitCloneWriterStepRequest(t, id, map[string]string{"RECEIPT": sampleGitCloneReceipt})

	resp, err := NewStepExecutorStateless().Run(context.Background(), req, cfg, nil, logstream.NopWriter())
	require.NoError(t, err)
	assert.Equal(t, sampleGitCloneReceipt, resp.GitCloneMetadata)
	_, statErr := os.Stat(gitCloneReceiptPath(id))
	assert.True(t, os.IsNotExist(statErr), "receipt must be removed after collection")
}
