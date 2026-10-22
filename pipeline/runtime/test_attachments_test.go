// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const attachmentReport = `<testsuites><testsuite name="pw">
<testcase name="login"><failure message="x"/>
<system-out>[[ATTACHMENT|results/login/trace.zip]]</system-out></testcase>
</testsuite></testsuites>`

func attachmentStep(t *testing.T, envs map[string]string) *api.StartStepRequest {
	t.Helper()
	ws, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(ws, "report.xml"), []byte(attachmentReport), 0o600))
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "results", "login"), 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(ws, "results", "login", "trace.zip"), []byte("trace"), 0o600))
	return &api.StartStepRequest{
		ID: "step1", WorkingDir: ws, Envs: envs,
		TestReport: api.TestReport{Kind: api.Junit, Junit: api.JunitReport{Paths: []string{"report.xml"}}},
	}
}

type recordingWriter struct {
	mockLogWriter
	bytes.Buffer
}

func (w *recordingWriter) Write(p []byte) (int, error) { return w.Buffer.Write(p) }

func TestUploadTestAttachments(t *testing.T) { //nolint:funlen
	var mu sync.Mutex
	var paths []string
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, r.URL.Path)
		auths = append(auths, r.Header.Get("Authorization"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	reset := func() {
		mu.Lock()
		defer mu.Unlock()
		paths, auths = nil, nil
	}
	harCfg := func(ws string) *api.HarConfig {
		return &api.HarConfig{URL: srv.URL, AccountID: "acc", Registry: "reg", PipelineUniqueID: "pipe",
			BuildID: "3", Token: "tok", Workspace: ws}
	}

	t.Run("uploads for a step that opted in", func(t *testing.T) {
		reset()
		var out bytes.Buffer
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		assert.True(t, uploadTestAttachments(context.Background(), r, harCfg(r.WorkingDir), &out))
		assert.Equal(t, []string{"/pkg/acc/reg/files/pipe/v3/test-attachments/step1/results/login/trace.zip"}, paths)
		assert.Equal(t, []string{"CIManager tok"}, auths)
		assert.Contains(t, out.String(), "1 uploaded, 0 failed")
	})

	t.Run("step env cannot change the registry, token or workspace", func(t *testing.T) {
		reset()
		evil := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
			t.Error("request sent to a server named by the step env")
		}))
		defer evil.Close()
		hostDir := t.TempDir()
		require.NoError(t, os.WriteFile(filepath.Join(hostDir, "host.txt"), []byte("host"), 0o600))
		r := attachmentStep(t, map[string]string{
			"PLUGIN_UPLOAD_ARTIFACTS": "all",
			"HARNESS_HAR_URL":         evil.URL, "HARNESS_HAR_TOKEN": "stolen", "CI_NATIVE_HAR_REGISTRY": "other",
			"HARNESS_WORKSPACE": "/", "DRONE_WORKSPACE": hostDir, "HARNESS_PIPELINE_UNIQUE_ID": "other",
			"HARNESS_ACCOUNT_ID": "other", "HARNESS_BUILD_ID": "99",
		})
		t.Setenv("HARNESS_WORKSPACE", "/")
		var out bytes.Buffer
		uploadTestAttachments(context.Background(), r, harCfg(r.WorkingDir), &out)
		assert.Equal(t, []string{"/pkg/acc/reg/files/pipe/v3/test-attachments/step1/results/login/trace.zip"}, paths)
		assert.Equal(t, []string{"CIManager tok"}, auths)
		assert.NotContains(t, out.String(), "stolen")
	})

	t.Run("uses the configured workspace", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		other, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		uploadTestAttachments(context.Background(), r, harCfg(other), &bytes.Buffer{})
		assert.Empty(t, paths, "files outside the configured workspace are not uploaded")
	})

	t.Run("nil config does nothing", func(t *testing.T) {
		reset()
		var out bytes.Buffer
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		assert.False(t, uploadTestAttachments(context.Background(), r, nil, &out))
		assert.Empty(t, paths)
		assert.Empty(t, out.String())
	})

	t.Run("partial config warns and does not upload", func(t *testing.T) {
		reset()
		var out bytes.Buffer
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		cfg := harCfg(r.WorkingDir)
		cfg.Registry = ""
		uploadTestAttachments(context.Background(), r, cfg, &out)
		assert.Empty(t, paths)
		assert.Contains(t, out.String(), "level=warning")
		assert.Contains(t, out.String(), "registry not set")
		assert.NotContains(t, out.String(), "tok")
	})

	t.Run("empty config is silent", func(t *testing.T) {
		reset()
		var out bytes.Buffer
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		uploadTestAttachments(context.Background(), r, &api.HarConfig{}, &out)
		assert.Empty(t, paths)
		assert.Empty(t, out.String())
	})

	t.Run("does nothing without the opt-in", func(t *testing.T) {
		reset()
		var out bytes.Buffer
		r := attachmentStep(t, map[string]string{})
		assert.False(t, uploadTestAttachments(context.Background(), r, harCfg(r.WorkingDir), &out))
		assert.Empty(t, paths)
		assert.Empty(t, out.String())
	})

	t.Run("does nothing without a report", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		r.TestReport = api.TestReport{}
		assert.False(t, uploadTestAttachments(context.Background(), r, harCfg(r.WorkingDir), &bytes.Buffer{}))
		assert.Empty(t, paths)
	})

	t.Run("does nothing once the step context is done", func(t *testing.T) {
		reset()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
		assert.False(t, uploadTestAttachments(ctx, r, harCfg(r.WorkingDir), &bytes.Buffer{}))
		assert.Empty(t, paths)
	})
}

func TestExecuteStepHelper_UploadDoesNotTimeOutPassedStep(t *testing.T) {
	// The server holds the upload until the step context deadline passes.
	srv := httptest.NewServer(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		_, _ = io.Copy(io.Discard, r.Body)
		select {
		case <-r.Context().Done():
		case <-time.After(10 * time.Second):
		}
	}))
	defer srv.Close()

	r := attachmentStep(t, map[string]string{"PLUGIN_UPLOAD_ARTIFACTS": "all"})
	r.Kind = api.Run
	r.Run = api.RunConfig{Command: []string{"echo", "hello"}}
	harCfg := &api.HarConfig{URL: srv.URL, AccountID: "acc", Registry: "reg", PipelineUniqueID: "pipe",
		BuildID: "3", Token: "tok", Workspace: r.WorkingDir}
	runFn := func(context.Context, *spec.Step, io.Writer, bool, bool) (*runtime.State, error) {
		return &runtime.State{Exited: true, ExitCode: 0}, nil
	}

	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	wr := &recordingWriter{}
	state, _, _, _, _, _, _, err := executeStepHelper(ctx, r, runFn, wr, newTestTiConfig(), false, false, harCfg) //nolint:dogsled
	require.NoError(t, err, "an upload cut short by the step deadline must not time out a passed step")
	require.NotNil(t, state)
	assert.Equal(t, 0, state.ExitCode)
	require.Error(t, ctx.Err(), "the step deadline passed during the upload")
	assert.Contains(t, wr.String(), "Test attachment upload failed")
}

func TestExecuteStepHelper_TimeoutWithoutUploadIsStillReported(t *testing.T) {
	r := &api.StartStepRequest{ID: "s", Kind: api.Run, Run: api.RunConfig{Command: []string{"echo", "hello"}}, Envs: map[string]string{}}
	runFn := func(ctx context.Context, _ *spec.Step, _ io.Writer, _ bool, _ bool) (*runtime.State, error) {
		<-ctx.Done()
		return &runtime.State{Exited: true, ExitCode: 0}, nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	_, _, _, _, _, _, _, err := executeStepHelper(ctx, r, runFn, &mockLogWriter{}, newTestTiConfig(), false, false, nil) //nolint:dogsled
	assert.ErrorIs(t, err, context.DeadlineExceeded)
}
