// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const junitReport = `<?xml version="1.0"?>
<testsuites>
  <testsuite name="pw">
    <testcase name="login" classname="a">
      <failure message="boom"/>
      <system-out>[[ATTACHMENT|test-results/login-chromium/trace.zip]]
[[ATTACHMENT|test-results/login-chromium-retry1/test-failed-1.png]]</system-out>
    </testcase>
    <testcase name="home" classname="a">
      <system-out>[[ATTACHMENT|test-results/home-chromium/video.webm]]</system-out>
    </testcase>
    <testcase name="plain" classname="a"><system-out>no attachments here</system-out></testcase>
  </testsuite>
</testsuites>`

const (
	reportFile   = "results.xml"
	homeBody     = "home"
	testRegistry = "reg"
	modeAll      = "all"
)

// newWorkspace writes the layout Playwright produces and returns the workspace path.
func newWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	// The temp dir can be a symlink (macOS), and the code compares real paths.
	ws, err := filepath.EvalSymlinks(ws)
	require.NoError(t, err)
	for name, content := range map[string]string{
		reportFile:                              junitReport,
		"test-results/login-chromium/trace.zip": "trace",
		"test-results/login-chromium-retry1/test-failed-1.png": "png",
		"test-results/login-chromium-retry1/video.webm":        "video",
		"test-results/home-chromium/video.webm":                homeBody,
	} {
		p := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(p), 0o755))
		require.NoError(t, os.WriteFile(p, []byte(content), 0o600))
	}
	return ws
}

type recorder struct {
	mu       sync.Mutex
	requests map[string]string // path -> body
	auth     map[string]string // path -> Authorization
}

// attemptDir matches the directory of one run of the step below the step id, which is random.
var attemptDir = regexp.MustCompile(`/step1/[0-9a-f]{16}/`)

func newServer(t *testing.T, status func(path string, attempt int) int) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{requests: map[string]string{}, auth: map[string]string{}}
	attempts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		path := attemptDir.ReplaceAllString(r.URL.Path, "/step1/")
		attempts[path]++
		code := http.StatusOK
		if status != nil {
			code = status(path, attempts[path])
		}
		if code < 400 {
			rec.requests[path] = string(body)
			rec.auth[path] = r.Header.Get("Authorization")
		}
		w.WriteHeader(code)
	}))
	t.Cleanup(srv.Close)
	return srv, rec
}

func (r *recorder) paths() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	for p := range r.requests {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// fastRetries shortens the pause between upload attempts for the test and restores it afterwards.
func fastRetries(t *testing.T) {
	t.Helper()
	old := retryBackoff
	retryBackoff = time.Millisecond
	t.Cleanup(func() { retryBackoff = old })
}

func testConfig(serverURL, ws string) *Config {
	return &Config{URL: serverURL, Registry: testRegistry, PipelineUniqueID: "pipe", Token: "secret-token",
		AccountID: "acc", BuildID: "7", Workspace: ws}
}

func runUpload(t *testing.T, cfg *Config, p *Params) string {
	t.Helper()
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	log.SetLevel(logrus.DebugLevel)
	if p.StepID == "" {
		p.StepID = "step1"
	}
	if p.WorkingDir == "" {
		p.WorkingDir = cfg.Workspace
	}
	p.ReportPaths = []string{reportFile}
	if p.Deadline.IsZero() {
		p.Deadline = time.Now().Add(time.Hour) // a step without a timeout uploads nothing
	}
	NewPlan(cfg, p, log).Upload(context.Background(), p.Deadline, log)
	return out.String()
}

func TestUploadAll(t *testing.T) {
	fastRetries(t)
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)

	out := runUpload(t, testConfig(srv.URL, ws), &Params{Mode: modeAll})

	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	// Only traces and videos are uploaded: the png of the retry is left out.
	assert.Equal(t, []string{
		prefix + "test-results/home-chromium/video.webm",
		prefix + "test-results/login-chromium-retry1/video.webm",
		prefix + "test-results/login-chromium/trace.zip",
	}, rec.paths())
	assert.Equal(t, "trace", rec.requests[prefix+"test-results/login-chromium/trace.zip"])
	assert.Equal(t, "CIManager secret-token", rec.auth[prefix+"test-results/login-chromium/trace.zip"])
	assert.Contains(t, out, "3 uploaded, 0 not confirmed as uploaded")
	assert.NotContains(t, out, "secret-token")
}

func TestUploadFailedMode(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "failed"})

	// homeBody passed and ran once, so only the failed (and retried) login test is uploaded.
	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	assert.Equal(t, []string{
		prefix + "test-results/login-chromium-retry1/video.webm",
		prefix + "test-results/login-chromium/trace.zip",
	}, rec.paths())
}

func TestUploadEmptyModeUploadsAll(t *testing.T) {
	srv, rec := newServer(t, nil)
	runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: ""})
	assert.Len(t, rec.paths(), 3)
}

func TestUploadSkips(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)

	for name, mode := range map[string]string{"mode none": "none", "invalid mode": "everything"} {
		t.Run(name, func(t *testing.T) {
			runUpload(t, testConfig(srv.URL, ws), &Params{Mode: mode})
			assert.Empty(t, rec.paths())
		})
	}
	for name, cfg := range map[string]Config{
		"no registry":    {},
		"partial config": {URL: srv.URL, Workspace: ws},
		"no token":       {URL: srv.URL, Registry: testRegistry, PipelineUniqueID: "p", AccountID: "a", BuildID: "1", Workspace: ws},
		"no account id":  {URL: srv.URL, Registry: testRegistry, PipelineUniqueID: "p", Token: "t", BuildID: "1", Workspace: ws},
		"no build id":    {URL: srv.URL, Registry: testRegistry, PipelineUniqueID: "p", Token: "t", AccountID: "a", Workspace: ws},
		"no workspace":   {URL: srv.URL, Registry: testRegistry, PipelineUniqueID: "p", Token: "t", AccountID: "a", BuildID: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			runUpload(t, &cfg, &Params{Mode: modeAll, WorkingDir: ws})
			assert.Empty(t, rec.paths())
		})
	}
}

func TestUploadQuietWhenNotConfigured(t *testing.T) {
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	NewPlan(&Config{Workspace: newWorkspace(t)},
		&Params{StepID: "s", WorkingDir: newWorkspace(t), ReportPaths: []string{reportFile}, Mode: modeAll}, log).
		Upload(context.Background(), time.Time{}, log)
	assert.Empty(t, out.String(), "nothing is logged to the step when the registry is not configured")
}

func TestUploadWarnsOnIncompleteConfig(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	cfg := testConfig(srv.URL, ws)
	cfg.AccountID = ""
	cfg.Workspace = ""
	out := runUpload(t, cfg, &Params{Mode: modeAll, WorkingDir: ws})
	assert.Contains(t, out, "configuration is incomplete")
	assert.Contains(t, out, "account_id, workspace not set")
	assert.NotContains(t, out, "secret-token")
	assert.Empty(t, rec.paths())
}

func TestUploadRetriesServerErrorsThenSucceeds(t *testing.T) {
	fastRetries(t)
	srv, rec := newServer(t, func(_ string, attempt int) int {
		if attempt < 3 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	out := runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: modeAll})
	assert.Len(t, rec.paths(), 3)
	assert.Contains(t, out, "3 uploaded, 0 not confirmed as uploaded")
}

func TestUploadDoesNotRetryClientErrors(t *testing.T) {
	fastRetries(t)
	var mu sync.Mutex
	calls := 0
	srv, _ := newServer(t, func(_ string, _ int) int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return http.StatusForbidden
	})
	out := runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: modeAll})
	assert.Equal(t, 3, calls, "one attempt per file")
	assert.Contains(t, out, "0 uploaded, 3 not confirmed as uploaded")
	assert.Contains(t, out, "403")
}

func TestUploadUsesOnlyTheGivenWorkspace(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	// A workspace other than the one the files are in selects nothing, wherever the step runs.
	other, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	t.Setenv("HARNESS_WORKSPACE", ws)
	t.Setenv("DRONE_WORKSPACE", ws)

	runUpload(t, testConfig(srv.URL, other), &Params{Mode: modeAll, WorkingDir: ws})
	assert.Empty(t, rec.paths())
}

func TestUploadIgnoresSymlinkToFileOutsideWorkspace(t *testing.T) {
	srv, rec := newServer(t, nil)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("host file"), 0o600))
	ws := newWorkspace(t)
	require.NoError(t, os.MkdirAll(filepath.Join(ws, "test-results", "login"), 0o755))
	require.NoError(t, os.Symlink(filepath.Join(outside, "secret.txt"), filepath.Join(ws, "test-results", "login", "trace.zip")))
	report := `<testsuites><testsuite><testcase name="a"><failure/>` +
		`<system-out>[[ATTACHMENT|test-results/login/trace.zip]]</system-out></testcase></testsuite></testsuites>`
	require.NoError(t, os.WriteFile(filepath.Join(ws, reportFile), []byte(report), 0o600))

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: modeAll})
	assert.Empty(t, rec.paths())
}

func TestUploadSameDirNameInDifferentParentsDoesNotCollide(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	for _, name := range []string{"app/test-results/login/trace.zip", "admin/test-results/login/trace.zip"} {
		path := filepath.Join(ws, filepath.FromSlash(name))
		require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
		require.NoError(t, os.WriteFile(path, []byte(name), 0o600))
	}
	report := `<testsuites><testsuite>` +
		`<testcase name="app"><failure/><system-out>[[ATTACHMENT|app/test-results/login/trace.zip]]</system-out></testcase>` +
		`<testcase name="admin"><failure/><system-out>[[ATTACHMENT|admin/test-results/login/trace.zip]]</system-out></testcase>` +
		`</testsuite></testsuites>`
	require.NoError(t, os.WriteFile(filepath.Join(ws, reportFile), []byte(report), 0o600))

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "failed"})

	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	assert.Contains(t, rec.paths(), prefix+"app/test-results/login/trace.zip")
	assert.Contains(t, rec.paths(), prefix+"admin/test-results/login/trace.zip")
}

func TestProjectRootRestrictsSelection(t *testing.T) {
	srv, rec := newServer(t, nil)
	// Only attachments inside the project root are considered.
	runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: modeAll, ProjectRoot: "elsewhere"})
	assert.Empty(t, rec.paths())
}

func TestProjectRootInsideWorkspaceIsUsed(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: modeAll, ProjectRoot: "test-results"})
	assert.NotEmpty(t, rec.paths())
}

func TestProjectRootOutsideWorkspaceIsRejected(t *testing.T) {
	srv, rec := newServer(t, nil)
	outside, err := filepath.EvalSymlinks(t.TempDir())
	require.NoError(t, err)
	hostFile := filepath.Join(outside, "results", "login", "host-secret.txt")
	require.NoError(t, os.MkdirAll(filepath.Dir(hostFile), 0o755))
	require.NoError(t, os.WriteFile(hostFile, []byte("host"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "results", "login", "trace.zip"), []byte("trace"), 0o600))
	ws := newWorkspace(t)
	report := `<testsuites><testsuite><testcase name="a"><failure/><system-out>[[ATTACHMENT|` +
		filepath.Join(outside, "results", "login", "trace.zip") + `]]</system-out></testcase></testsuite></testsuites>`
	require.NoError(t, os.WriteFile(filepath.Join(ws, reportFile), []byte(report), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "escape")))

	for _, root := range []string{outside, string(filepath.Separator), "..", "escape"} {
		out := runUpload(t, testConfig(srv.URL, ws), &Params{Mode: modeAll, ProjectRoot: root})
		assert.Empty(t, rec.paths(), root)
		assert.Contains(t, out, "the project root", root)
		assert.Contains(t, out, "is not inside the workspace", root)
		assert.NotContains(t, out, "host-secret.txt", root)
	}
}

func TestInvalidStepIDIsRejected(t *testing.T) {
	srv, rec := newServer(t, nil)
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	for _, id := range []string{"", "..", "a/b", `a\b`} {
		ws := newWorkspace(t)
		NewPlan(testConfig(srv.URL, ws),
			&Params{StepID: id, WorkingDir: ws, ReportPaths: []string{reportFile}, Mode: modeAll}, log).
			Upload(context.Background(), time.Time{}, log)
	}
	assert.Empty(t, rec.paths())
}

func newPlanFor(t *testing.T, ws string, p *Params) (plan *Plan, logged string) {
	t.Helper()
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	log.SetLevel(logrus.DebugLevel)
	p.StepID = "step1"
	p.WorkingDir = ws
	p.ReportPaths = []string{reportFile}
	if p.Deadline.IsZero() {
		p.Deadline = time.Now().Add(time.Hour)
	}
	return NewPlan(testConfig("http://unused.invalid", ws), p, log), out.String()
}

func TestAnnotateLinksTheTestsToThePlannedFiles(t *testing.T) {
	ws := newWorkspace(t)
	plan, _ := newPlanFor(t, ws, &Params{Mode: modeAll})
	require.NotNil(t, plan)

	login := &types.TestCase{Name: "login", ClassName: "a"}
	plan.Annotate(filepath.Join(ws, reportFile), "[[ATTACHMENT|test-results/login-chromium/trace.zip]]\n"+
		"[[ATTACHMENT|test-results/login-chromium-retry1/test-failed-1.png]]", login)

	// The path is <step id>/<attempt>/<result dir>/<file>, the way the Kubernetes addon links it. The png is not
	// planned, so only the trace of the first run and the video of the retry are linked.
	require.Len(t, login.Artifacts, 2)
	first, retry := login.Artifacts[0], login.Artifacts[1]
	assert.Equal(t, types.TestArtifactTypeTrace, first.Type)
	assert.Equal(t, "step1/"+plan.attemptID+"/test-results/login-chromium/trace.zip", first.Path)
	assert.Equal(t, int64(len("trace")), first.Size)
	assert.Equal(t, 0, first.Attempt)
	assert.Equal(t, types.TestArtifactTypeVideo, retry.Type)
	assert.Equal(t, "step1/"+plan.attemptID+"/test-results/login-chromium-retry1/video.webm", retry.Path)
	assert.Equal(t, 1, retry.Attempt)
}

func TestAnnotateIgnoresTestsWithoutAttachmentsAndRepeats(t *testing.T) {
	ws := newWorkspace(t)
	plan, _ := newPlanFor(t, ws, &Params{Mode: modeAll})
	require.NotNil(t, plan)
	junit := filepath.Join(ws, reportFile)

	plain := &types.TestCase{Name: "plain"}
	plan.Annotate(junit, "no attachments here", plain)
	assert.Empty(t, plain.Artifacts)

	outside := &types.TestCase{Name: "outside"}
	plan.Annotate(junit, "[[ATTACHMENT|../elsewhere/trace.zip]]", outside)
	assert.Empty(t, outside.Artifacts)

	home := &types.TestCase{Name: homeBody, ClassName: "a"}
	plan.Annotate(junit, "[[ATTACHMENT|test-results/home-chromium/video.webm]]", home)
	require.Len(t, home.Artifacts, 1)
	// The same test listed again, for example by a second report, gets no second copy.
	again := &types.TestCase{Name: homeBody, ClassName: "a"}
	plan.Annotate(junit, "[[ATTACHMENT|test-results/home-chromium/video.webm]]", again)
	assert.Empty(t, again.Artifacts)
}

func TestAnnotateOnlyLinksTheSelectedTests(t *testing.T) {
	ws := newWorkspace(t)
	plan, _ := newPlanFor(t, ws, &Params{Mode: "failed"})
	require.NotNil(t, plan)

	// homeBody passed on its first run, so its video is not uploaded and must not be linked.
	home := &types.TestCase{Name: homeBody, ClassName: "a"}
	plan.Annotate(filepath.Join(ws, reportFile), "[[ATTACHMENT|test-results/home-chromium/video.webm]]", home)
	assert.Empty(t, home.Artifacts)
}

func TestAnnotateOnNilPlanDoesNothing(t *testing.T) {
	var plan *Plan
	tc := &types.TestCase{Name: "x"}
	plan.Annotate(reportFile, "[[ATTACHMENT|a/trace.zip]]", tc)
	assert.Empty(t, tc.Artifacts)
}

func TestEachPlanUploadsToItsOwnAttemptDirectory(t *testing.T) {
	ws := newWorkspace(t)
	first, _ := newPlanFor(t, ws, &Params{Mode: modeAll})
	second, _ := newPlanFor(t, ws, &Params{Mode: modeAll})
	require.NotNil(t, first)
	require.NotNil(t, second)
	// A step that runs again, for example under a retry strategy, keeps its step id, so the registry would refuse
	// the files of the second run without a directory of its own.
	assert.NotEqual(t, first.attemptID, second.attemptID)
}

func TestStepWithoutTimeoutIsNotPlanned(t *testing.T) {
	ws := newWorkspace(t)
	srv, rec := newServer(t, nil)
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	plan := NewPlan(testConfig(srv.URL, ws), &Params{StepID: "step1", WorkingDir: ws,
		ReportPaths: []string{reportFile}, Mode: modeAll}, log)
	assert.Nil(t, plan, "like in Kubernetes, a step without a deadline has no time to upload")
	assert.Contains(t, out.String(), "too little step time was left")
	assert.Empty(t, rec.paths())
}

func TestNoUploadTimeLeftSkipsThePlan(t *testing.T) {
	ws := newWorkspace(t)
	plan, out := newPlanFor(t, ws, &Params{Mode: modeAll, Deadline: time.Now().Add(time.Minute)})
	assert.Nil(t, plan)
	assert.Contains(t, out, "too little step time was left")
	assert.Contains(t, out, "upload_artifacts")

	plan, _ = newPlanFor(t, ws, &Params{Mode: modeAll, Deadline: time.Now().Add(time.Hour)})
	assert.NotNil(t, plan)
}

func TestRetryAttempt(t *testing.T) {
	assert.Equal(t, 0, retryAttempt("test-results/login/trace.zip"))
	assert.Equal(t, 2, retryAttempt("test-results/login-retry2/trace.zip"))
	assert.Equal(t, 1, retryAttempt("test-results/login-retry1/attachments/video.webm"))
}

func TestTestArtifactType(t *testing.T) {
	assert.Equal(t, types.TestArtifactTypeTrace, testArtifactType("a/trace.zip"))
	assert.Equal(t, types.TestArtifactTypeVideo, testArtifactType("a/Video.WEBM"))
	assert.Empty(t, testArtifactType("a/test-failed-1.png"))
	assert.Empty(t, testArtifactType("a/error-context.md"))
}

func TestLinkedAndFileCount(t *testing.T) {
	var nilPlan *Plan
	assert.False(t, nilPlan.Linked())
	assert.Zero(t, nilPlan.FileCount())

	ws := newWorkspace(t)
	plan, _ := newPlanFor(t, ws, &Params{Mode: modeAll})
	require.NotNil(t, plan)
	assert.Positive(t, plan.FileCount())
	assert.False(t, plan.Linked(), "no test was linked yet")

	plan.Annotate(filepath.Join(ws, reportFile), "[[ATTACHMENT|test-results/login-chromium/trace.zip]]",
		&types.TestCase{Name: "login", ClassName: "a"})
	assert.True(t, plan.Linked())
}

func TestUploadEndsAtTheStepDeadlineAndSaysSo(t *testing.T) {
	fastRetries(t)
	oldMin, oldReserve := minUploadTime, stepEndReserve
	minUploadTime, stepEndReserve = 0, 0
	t.Cleanup(func() { minUploadTime, stepEndReserve = oldMin, oldReserve })

	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
	}))
	t.Cleanup(slow.Close)
	ws := newWorkspace(t)
	start := time.Now()
	out := runUpload(t, testConfig(slow.URL, ws), &Params{Mode: modeAll, Deadline: time.Now().Add(300 * time.Millisecond)})
	assert.Less(t, time.Since(start), 3*time.Second, "the upload must stop at the step deadline")
	assert.Contains(t, out, "because the step timeout was reached")
	assert.Contains(t, out, "upload_artifacts (PLUGIN_UPLOAD_ARTIFACTS)")
	assert.Contains(t, out, "0 uploaded")
}

func TestUploadGuardedTurnsAPanicIntoAnError(t *testing.T) {
	err := uploadGuarded(context.Background(), nil, upload{artifactPath: "a/b"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "upload panicked")
}
