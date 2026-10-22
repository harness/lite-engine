// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"bytes"
	"compress/gzip"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	runnerRuntime "github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/common/external"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/pipeline"
	"github.com/harness/lite-engine/ti/attachments"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/ti-client/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testAccount  = "acc"
	testToken    = "tok"
	otherValue   = "other"
	testPipeline = "pipe"
	testRegistry = "reg"
	modeAll      = "all"
)

const attachmentReport = `<testsuites><testsuite name="pw">
<testcase name="login"><failure message="x"/>
<system-out>[[ATTACHMENT|results/login/trace.zip]]</system-out></testcase>
</testsuite></testsuites>`

// attemptDir matches the directory of one run of the step below the step id, which is random.
var attemptDir = regexp.MustCompile(`/step1/[0-9a-f]{16}/`)

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

// attach plans, links and uploads the attachments of the step the way executeRunStep does, and returns the
// test cases of the report and what the step log got.
func attach(ctx context.Context, r *api.StartStepRequest) (tests []*types.TestCase, logged string) {
	// A step without a timeout uploads nothing, so a context without a deadline gets one.
	if _, ok := ctx.Deadline(); !ok {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, time.Hour)
		defer cancel()
	}
	var out bytes.Buffer
	plan := planTestAttachments(ctx, r, &out)
	if plan != nil {
		tc := &types.TestCase{Name: "login"}
		attachmentAnnotator(plan)(filepath.Join(r.WorkingDir, "report.xml"), "[[ATTACHMENT|results/login/trace.zip]]", tc)
		tests = append(tests, tc)
	}
	uploadTestAttachments(ctx, plan, nil, &out)
	return tests, out.String()
}

func testHarConfig(url, workspace string) api.HarConfig {
	return api.HarConfig{URL: url, AccountID: testAccount, Registry: testRegistry, PipelineUniqueID: testPipeline,
		BuildID: "3", Token: testToken, Workspace: workspace}
}

func TestUploadTestAttachments(t *testing.T) { //nolint:funlen
	var mu sync.Mutex
	var paths []string
	var auths []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		paths = append(paths, attemptDir.ReplaceAllString(r.URL.Path, "/step1/"))
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
	harCfg := func(ws string) api.HarConfig { return testHarConfig(srv.URL, ws) }

	t.Run("links and uploads for a step that opted in", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
		r.HarConfig = harCfg(r.WorkingDir)
		tests, out := attach(context.Background(), r)
		assert.Equal(t, []string{"/pkg/acc/reg/files/pipe/v3/test-attachments/step1/results/login/trace.zip"}, paths)
		assert.Equal(t, []string{"CIManager tok"}, auths)
		assert.Contains(t, out, "1 uploaded, 0 not confirmed as uploaded")
		require.Len(t, tests, 1)
		require.Len(t, tests[0].Artifacts, 1)
		assert.Equal(t, types.TestArtifactTypeTrace, tests[0].Artifacts[0].Type)
		assert.Regexp(t, `^step1/[0-9a-f]{16}/results/login/trace\.zip$`, tests[0].Artifacts[0].Path)
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
			attachments.UploadArtifactsEnv: modeAll,
			"HARNESS_HAR_URL":              evil.URL, "HARNESS_HAR_TOKEN": "stolen", "CI_NATIVE_HAR_REGISTRY": otherValue,
			"HARNESS_WORKSPACE": "/", "DRONE_WORKSPACE": hostDir, "HARNESS_PIPELINE_UNIQUE_ID": otherValue,
			"HARNESS_ACCOUNT_ID": otherValue, "HARNESS_BUILD_ID": "99",
		})
		r.HarConfig = harCfg(r.WorkingDir)
		t.Setenv("HARNESS_WORKSPACE", "/")
		_, out := attach(context.Background(), r)
		assert.Equal(t, []string{"/pkg/acc/reg/files/pipe/v3/test-attachments/step1/results/login/trace.zip"}, paths)
		assert.Equal(t, []string{"CIManager tok"}, auths)
		assert.NotContains(t, out, "stolen")
	})

	t.Run("uses the configured workspace", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
		other, err := filepath.EvalSymlinks(t.TempDir())
		require.NoError(t, err)
		r.HarConfig = harCfg(other)
		attach(context.Background(), r)
		assert.Empty(t, paths, "files outside the configured workspace are not uploaded")
	})

	t.Run("does nothing without the opt-in", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{})
		r.HarConfig = harCfg(r.WorkingDir)
		tests, out := attach(context.Background(), r)
		assert.Empty(t, paths)
		assert.Empty(t, tests)
		assert.Empty(t, out)
	})

	t.Run("does nothing without a report", func(t *testing.T) {
		reset()
		r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
		r.HarConfig = harCfg(r.WorkingDir)
		r.TestReport = api.TestReport{}
		attach(context.Background(), r)
		assert.Empty(t, paths)
	})

	t.Run("does nothing once the step context is done", func(t *testing.T) {
		reset()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
		r.HarConfig = harCfg(r.WorkingDir)
		attach(ctx, r)
		assert.Empty(t, paths)
	})

	t.Run("a step with too little time left is not planned", func(t *testing.T) {
		reset()
		ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
		defer cancel()
		r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
		r.HarConfig = harCfg(r.WorkingDir)
		tests, out := attach(ctx, r)
		assert.Empty(t, paths)
		assert.Empty(t, tests, "files that would not be uploaded must not be linked")
		assert.Contains(t, out, "too little step time was left")
	})
}

func TestUploadTestAttachmentsSkipsWhenTheReportLinksNothing(t *testing.T) {
	var mu sync.Mutex
	requests := 0
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		mu.Lock()
		requests++
		mu.Unlock()
	}))
	defer srv.Close()
	r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
	r.HarConfig = testHarConfig(srv.URL, r.WorkingDir)

	var out bytes.Buffer
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	plan := planTestAttachments(ctx, r, &out)
	require.NotNil(t, plan)

	// No test was annotated, for example because the report could not be parsed.
	uploadTestAttachments(ctx, plan, nil, &out)
	assert.Contains(t, out.String(), "Test attachment upload skipped: the test report was not written or links none of them")
	assert.Contains(t, out.String(), "1 files were not uploaded")

	// The report was linked, but writing it to the TI service failed.
	out.Reset()
	attachmentAnnotator(plan)(filepath.Join(r.WorkingDir, "report.xml"), "[[ATTACHMENT|results/login/trace.zip]]",
		&types.TestCase{Name: "login"})
	uploadTestAttachments(ctx, plan, assert.AnError, &out)
	assert.Contains(t, out.String(), "Test attachment upload skipped")

	mu.Lock()
	defer mu.Unlock()
	assert.Zero(t, requests)
}

func TestPlanTestAttachmentsSkipsDetachedSteps(t *testing.T) {
	r := attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll})
	r.HarConfig = testHarConfig("http://unused.invalid", r.WorkingDir)
	r.Detach = true
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	assert.Nil(t, planTestAttachments(ctx, r, &bytes.Buffer{}))
}

func TestMaskHarToken(t *testing.T) {
	r := &api.StartStepRequest{Secrets: []string{otherValue}, HarConfig: api.HarConfig{Token: testToken}}
	maskHarToken(r)
	assert.Equal(t, []string{otherValue, testToken}, r.Secrets)
	assert.NotContains(t, external.MaskString("PUT failed: Authorization CIManager "+testToken, r.Secrets), testToken,
		"the secrets of the step mask the token in the step log")

	padded := &api.StartStepRequest{HarConfig: api.HarConfig{Token: " " + testToken + "\n"}}
	maskHarToken(padded)
	assert.Equal(t, []string{testToken}, padded.Secrets, "masks the trimmed token that is sent")

	none := &api.StartStepRequest{Secrets: []string{otherValue}}
	maskHarToken(none)
	assert.Equal(t, []string{otherValue}, none.Secrets)
}

// stepRun is what a run of executeRunStep sent to the TI service and the registry, in order, and what the step
// log got.
type stepRun struct {
	events     []string
	reportBody string
	out        string
	err        error
}

// driveExecuteRunStep runs the real executeRunStep with the step function run against a fake TI service and a fake
// registry, which the request gets as its registry config.
func driveExecuteRunStep(t *testing.T, r *api.StartStepRequest, run RunFunc) *stepRun {
	t.Helper()
	t.Setenv("HARNESS_WORKDIR", t.TempDir())
	require.NoError(t, os.MkdirAll(pipeline.GetSharedVolPath(), 0o755))

	var mu sync.Mutex
	res := &stepRun{}
	ti := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if zr, err := gzip.NewReader(bytes.NewReader(body)); err == nil { // the TI client may compress the report
			body, _ = io.ReadAll(zr)
		}
		mu.Lock()
		defer mu.Unlock()
		if strings.Contains(string(body), "trace.zip") {
			res.events = append(res.events, "report")
			res.reportBody = string(body)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer ti.Close()
	har := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		res.events = append(res.events, "upload "+attemptDir.ReplaceAllString(r.URL.Path, "/step1/"))
		mu.Unlock()
		w.WriteHeader(http.StatusOK)
	}))
	defer har.Close()

	r.Name = "playwright"
	r.HarConfig = testHarConfig(har.URL, r.WorkingDir)
	tiConfig := tiCfg.New(ti.URL, "ti-token", testAccount, "org", "proj", testPipeline, "3", "stage", "repo", "sha", "", "", "", "", "", "", false, "", "")
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	var out bytes.Buffer
	_, _, _, _, _, _, _, res.err = executeRunStep(ctx, run, r, &out, &tiConfig) //nolint:dogsled

	mu.Lock()
	defer mu.Unlock()
	res.out = out.String()
	return res
}

func exitedStep(context.Context, *spec.Step, io.Writer, bool, bool) (*runnerRuntime.State, error) {
	return &runnerRuntime.State{Exited: true}, nil
}

// TestExecuteRunStepWritesTheReportBeforeUploadingAttachments drives the real executeRunStep: the test report the TI
// service gets links the files, and the files are uploaded to the registry only after that.
func TestExecuteRunStepWritesTheReportBeforeUploadingAttachments(t *testing.T) {
	res := driveExecuteRunStep(t, attachmentStep(t, map[string]string{attachments.UploadArtifactsEnv: modeAll}), exitedStep)
	require.NoError(t, res.err)
	assert.Equal(t, []string{"report", "upload /pkg/acc/reg/files/pipe/v3/test-attachments/step1/results/login/trace.zip"},
		res.events, "the report must reach the TI service before the files are uploaded")
	assert.Regexp(t, `step1/[0-9a-f]{16}/results/login/trace\.zip`, res.reportBody)
	assert.Contains(t, res.out, "1 uploaded, 0 not confirmed as uploaded")
}

// A step that did not set upload_artifacts behaves as before even if CI Manager sent a registry config: the report
// has no artifacts, nothing is uploaded and the step log has no attachment lines.
func TestExecuteRunStepWithoutOptInIgnoresTheRegistryConfig(t *testing.T) {
	res := driveExecuteRunStep(t, attachmentStep(t, map[string]string{}), exitedStep)
	require.NoError(t, res.err)
	assert.Equal(t, []string{"report"}, res.events, "the report is written, and nothing is uploaded")
	assert.NotRegexp(t, `step1/[0-9a-f]{16}`, res.reportBody, "the report links no files")
	assert.NotContains(t, res.out, "Test attachment")
}
