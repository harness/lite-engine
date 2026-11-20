// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/errors"
	"github.com/harness/lite-engine/logstream"
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

const sampleExplicitGitCloneReceipt = `{"repoUrl":"https://github.com/acme/repo.git","commitSha":"abc123","linesOfCode":42}`

func explicitGitCloneReceiptPath(stepID string) string {
	return fmt.Sprintf("%s/%s-explicit-git-clone-metadata.json", pipeline.GetSharedVolPath(), stepID)
}

func writeExplicitGitCloneReceipt(t *testing.T, stepID, content string) {
	t.Helper()
	path := explicitGitCloneReceiptPath(stepID)
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	require.NoError(t, os.WriteFile(path, []byte(content), 0o644))
	t.Cleanup(func() { _ = os.Remove(path) })
}

func TestReadExplicitGitCloneMetadata_FilePresent_ReturnsRawJSON(t *testing.T) {
	writeExplicitGitCloneReceipt(t, "egc-present", sampleExplicitGitCloneReceipt)
	assert.Equal(t, sampleExplicitGitCloneReceipt, readExplicitGitCloneMetadata("egc-present"))
}

func TestReadExplicitGitCloneMetadata_FileMissing_ReturnsEmpty(t *testing.T) {
	assert.Empty(t, readExplicitGitCloneMetadata("egc-does-not-exist"))
}

func TestReadExplicitGitCloneMetadata_EmptyFile_ReturnsEmpty(t *testing.T) {
	writeExplicitGitCloneReceipt(t, "egc-empty", "")
	assert.Empty(t, readExplicitGitCloneMetadata("egc-empty"))
}

func TestReadExplicitGitCloneMetadata_IsolatedPerStep(t *testing.T) {
	writeExplicitGitCloneReceipt(t, "egc-a", `{"commitSha":"aaa"}`)
	writeExplicitGitCloneReceipt(t, "egc-b", `{"commitSha":"bbb"}`)
	a := readExplicitGitCloneMetadata("egc-a")
	b := readExplicitGitCloneMetadata("egc-b")
	assert.Contains(t, a, "aaa")
	assert.NotContains(t, a, "bbb")
	assert.Contains(t, b, "bbb")
	assert.NotContains(t, b, "aaa")
}

func TestReadExplicitGitCloneMetadata_DoesNotParse(t *testing.T) {
	writeExplicitGitCloneReceipt(t, "egc-raw", "not json at all")
	assert.Equal(t, "not json at all", readExplicitGitCloneMetadata("egc-raw"))
}

func TestConvertStatus_CarriesExplicitGitCloneMetadata(t *testing.T) {
	resp := convertStatus(StepStatus{
		Status:                   Complete,
		State:                    &runtime.State{Exited: true, ExitCode: 0},
		ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt,
	})
	assert.Equal(t, sampleExplicitGitCloneReceipt, resp.ExplicitGitCloneMetadata)
}

func TestConvertStatus_NoMetadata_LeavesFieldEmpty(t *testing.T) {
	resp := convertStatus(StepStatus{Status: Complete, State: &runtime.State{Exited: true}})
	assert.Empty(t, resp.ExplicitGitCloneMetadata)
}

func TestConvertPollResponse_SuccessCarriesMetadata(t *testing.T) {
	poll := &api.PollStepResponse{ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt}
	vm := convertPollResponse(poll, nil)
	assert.Equal(t, api.Success, vm.CommandExecutionStatus)
	assert.Equal(t, sampleExplicitGitCloneReceipt, vm.ExplicitGitCloneMetadata)
}

func TestConvertPollResponse_FailureCarriesMetadata(t *testing.T) {
	// nil envs -> TestSummaryAsOutputEnabled false -> plain-failure branch
	poll := &api.PollStepResponse{Error: "boom", ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt}
	vm := convertPollResponse(poll, nil)
	assert.Equal(t, api.Failure, vm.CommandExecutionStatus)
	assert.Equal(t, sampleExplicitGitCloneReceipt, vm.ExplicitGitCloneMetadata)
}

func TestPollStepResponse_JSONKey_ExplicitGitCloneMetadata(t *testing.T) {
	with, err := json.Marshal(&api.PollStepResponse{ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt})
	require.NoError(t, err)
	assert.Contains(t, string(with), `"explicit_git_clone_metadata"`)

	without, err := json.Marshal(&api.PollStepResponse{})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "explicit_git_clone_metadata")
}

func TestVMTaskExecutionResponse_JSONKey_ExplicitGitCloneMetadata(t *testing.T) {
	// The delegate's VmTaskExecutionResponse bean binds to exactly this wire key.
	with, err := json.Marshal(api.VMTaskExecutionResponse{ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt})
	require.NoError(t, err)
	assert.Contains(t, string(with), `"explicit_git_clone_metadata"`)

	without, err := json.Marshal(api.VMTaskExecutionResponse{})
	require.NoError(t, err)
	assert.NotContains(t, string(without), "explicit_git_clone_metadata")
}

func TestConvertPollResponse_FailureWithTestSummaryCarriesMetadata(t *testing.T) {
	envs := map[string]string{"HARNESS_CI_TEST_SUMMARY_OUTPUT_FF": "true"}
	poll := &api.PollStepResponse{Error: "boom", ExplicitGitCloneMetadata: sampleExplicitGitCloneReceipt}
	vm := convertPollResponse(poll, envs)
	assert.Equal(t, api.Failure, vm.CommandExecutionStatus)
	assert.Equal(t, sampleExplicitGitCloneReceipt, vm.ExplicitGitCloneMetadata)
}
