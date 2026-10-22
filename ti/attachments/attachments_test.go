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
	"sort"
	"sync"
	"testing"
	"time"

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

// newWorkspace writes the layout Playwright produces and returns the workspace path.
func newWorkspace(t *testing.T) string {
	t.Helper()
	ws := t.TempDir()
	// The temp dir can be a symlink (macOS), and the code compares real paths.
	ws, err := filepath.EvalSymlinks(ws)
	require.NoError(t, err)
	for name, content := range map[string]string{
		"results.xml":                                          junitReport,
		"test-results/login-chromium/trace.zip":                "trace",
		"test-results/login-chromium-retry1/test-failed-1.png": "png",
		"test-results/login-chromium-retry1/video.webm":        "video",
		"test-results/home-chromium/video.webm":                "home",
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

func newServer(t *testing.T, status func(path string, attempt int) int) (*httptest.Server, *recorder) {
	t.Helper()
	rec := &recorder{requests: map[string]string{}, auth: map[string]string{}}
	attempts := map[string]int{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		rec.mu.Lock()
		defer rec.mu.Unlock()
		attempts[r.URL.Path]++
		code := http.StatusOK
		if status != nil {
			code = status(r.URL.Path, attempts[r.URL.Path])
		}
		if code < 400 {
			rec.requests[r.URL.Path] = string(body)
			rec.auth[r.URL.Path] = r.Header.Get("Authorization")
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

func testConfig(serverURL, ws string) *Config {
	return &Config{URL: serverURL, Registry: "reg", PipelineUniqueID: "pipe", Token: "secret-token",
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
	p.ReportPaths = []string{"results.xml"}
	Upload(context.Background(), cfg, p, log)
	return out.String()
}

func TestUploadAll(t *testing.T) {
	retryBackoff = time.Millisecond
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)

	out := runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "all"})

	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	assert.Equal(t, []string{
		prefix + "test-results/home-chromium/video.webm",
		prefix + "test-results/login-chromium-retry1/test-failed-1.png",
		prefix + "test-results/login-chromium-retry1/video.webm",
		prefix + "test-results/login-chromium/trace.zip",
	}, rec.paths())
	assert.Equal(t, "trace", rec.requests[prefix+"test-results/login-chromium/trace.zip"])
	assert.Equal(t, "CIManager secret-token", rec.auth[prefix+"test-results/login-chromium/trace.zip"])
	assert.Contains(t, out, "4 uploaded, 0 failed")
	assert.NotContains(t, out, "secret-token")
}

func TestUploadFailedMode(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "failed"})

	// "home" passed and ran once, so only the failed (and retried) login test is uploaded.
	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	assert.Equal(t, []string{
		prefix + "test-results/login-chromium-retry1/test-failed-1.png",
		prefix + "test-results/login-chromium-retry1/video.webm",
		prefix + "test-results/login-chromium/trace.zip",
	}, rec.paths())
}

func TestUploadEmptyModeUploadsAll(t *testing.T) {
	srv, rec := newServer(t, nil)
	runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: ""})
	assert.Len(t, rec.paths(), 4)
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
		"no token":       {URL: srv.URL, Registry: "reg", PipelineUniqueID: "p", AccountID: "a", BuildID: "1", Workspace: ws},
		"no account id":  {URL: srv.URL, Registry: "reg", PipelineUniqueID: "p", Token: "t", BuildID: "1", Workspace: ws},
		"no build id":    {URL: srv.URL, Registry: "reg", PipelineUniqueID: "p", Token: "t", AccountID: "a", Workspace: ws},
		"no workspace":   {URL: srv.URL, Registry: "reg", PipelineUniqueID: "p", Token: "t", AccountID: "a", BuildID: "1"},
	} {
		t.Run(name, func(t *testing.T) {
			runUpload(t, &cfg, &Params{Mode: "all", WorkingDir: ws})
			assert.Empty(t, rec.paths())
		})
	}
}

func TestUploadQuietWhenNotConfigured(t *testing.T) {
	var out bytes.Buffer
	log := logrus.New()
	log.Out = &out
	Upload(context.Background(), &Config{Workspace: newWorkspace(t)},
		&Params{StepID: "s", WorkingDir: newWorkspace(t), ReportPaths: []string{"results.xml"}, Mode: "all"}, log)
	assert.Empty(t, out.String(), "nothing is logged to the step when the registry is not configured")
}

func TestUploadWarnsOnIncompleteConfig(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	cfg := testConfig(srv.URL, ws)
	cfg.AccountID = ""
	cfg.Workspace = ""
	out := runUpload(t, cfg, &Params{Mode: "all", WorkingDir: ws})
	assert.Contains(t, out, "configuration is incomplete")
	assert.Contains(t, out, "account_id, workspace not set")
	assert.NotContains(t, out, "secret-token")
	assert.Empty(t, rec.paths())
}

func TestUploadRetriesServerErrorsThenSucceeds(t *testing.T) {
	retryBackoff = time.Millisecond
	srv, rec := newServer(t, func(_ string, attempt int) int {
		if attempt < 3 {
			return http.StatusServiceUnavailable
		}
		return http.StatusOK
	})
	out := runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: "all"})
	assert.Len(t, rec.paths(), 4)
	assert.Contains(t, out, "4 uploaded, 0 failed")
}

func TestUploadDoesNotRetryClientErrors(t *testing.T) {
	retryBackoff = time.Millisecond
	var mu sync.Mutex
	calls := 0
	srv, _ := newServer(t, func(_ string, _ int) int {
		mu.Lock()
		defer mu.Unlock()
		calls++
		return http.StatusForbidden
	})
	out := runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: "all"})
	assert.Equal(t, 4, calls, "one attempt per file")
	assert.Contains(t, out, "0 uploaded, 4 failed")
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

	runUpload(t, testConfig(srv.URL, other), &Params{Mode: "all", WorkingDir: ws})
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
	require.NoError(t, os.WriteFile(filepath.Join(ws, "results.xml"), []byte(report), 0o600))

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "all"})
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
	require.NoError(t, os.WriteFile(filepath.Join(ws, "results.xml"), []byte(report), 0o600))

	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "failed"})

	prefix := "/pkg/acc/reg/files/pipe/v7/test-attachments/step1/"
	assert.Contains(t, rec.paths(), prefix+"app/test-results/login/trace.zip")
	assert.Contains(t, rec.paths(), prefix+"admin/test-results/login/trace.zip")
}

func TestProjectRootRestrictsSelection(t *testing.T) {
	srv, rec := newServer(t, nil)
	// Only attachments inside the project root are considered.
	runUpload(t, testConfig(srv.URL, newWorkspace(t)), &Params{Mode: "all", ProjectRoot: "elsewhere"})
	assert.Empty(t, rec.paths())
}

func TestProjectRootInsideWorkspaceIsUsed(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "all", ProjectRoot: "test-results"})
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
	require.NoError(t, os.WriteFile(filepath.Join(ws, "results.xml"), []byte(report), 0o600))
	require.NoError(t, os.Symlink(outside, filepath.Join(ws, "escape")))

	for _, root := range []string{outside, string(filepath.Separator), "..", "escape"} {
		out := runUpload(t, testConfig(srv.URL, ws), &Params{Mode: "all", ProjectRoot: root})
		assert.Empty(t, rec.paths(), root)
		assert.Contains(t, out, "invalid PLUGIN_PROJECT_ROOT", root)
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
		Upload(context.Background(), testConfig(srv.URL, ws),
			&Params{StepID: id, WorkingDir: ws, ReportPaths: []string{"results.xml"}, Mode: "all"}, log)
	}
	assert.Empty(t, rec.paths())
}
