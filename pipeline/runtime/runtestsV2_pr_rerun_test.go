// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package runtime

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/internal/filesystem"
	tiCfg "github.com/harness/lite-engine/ti/config"
	"github.com/harness/lite-engine/ti/instrumentation"
	tiClientTypes "github.com/harness/ti-client/chrysalis/types"
	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_getPRChangedFilePaths(t *testing.T) {
	files := []types.File{
		{Name: "src/A.java", Status: types.FileModified},
		{Name: "src/B.java", Status: types.FileAdded},
		{Name: "src/Old.java", Status: types.FileDeleted}, // rename source
		{Name: "src/New.java", Status: types.FileAdded},   // rename target
		{Name: "", Status: types.FileModified},
	}
	assert.Equal(t, []string{"src/A.java", "src/B.java", "src/New.java"}, getPRChangedFilePaths(files))
	assert.Empty(t, getPRChangedFilePaths(nil))
	assert.Empty(t, getPRChangedFilePaths([]types.File{{Name: "x", Status: types.FileDeleted}}))
}

// prTestRepo is a git repo whose last checkout move leaves `HEAD@{1}` on the first commit, i.e. a PR with changes.
type prTestRepo struct {
	t   *testing.T
	dir string
}

func (r *prTestRepo) git(args ...string) {
	r.t.Helper()
	cmd := exec.Command("git", append([]string{"-c", "user.email=t@t", "-c", "user.name=t"}, args...)...)
	cmd.Dir = r.dir
	cmd.Env = append(os.Environ(), "GIT_CONFIG_GLOBAL=/dev/null", "GIT_CONFIG_SYSTEM=/dev/null")
	out, err := cmd.CombinedOutput()
	require.NoError(r.t, err, string(out))
}

func (r *prTestRepo) write(name, content string) {
	r.t.Helper()
	p := filepath.Join(r.dir, name)
	require.NoError(r.t, os.MkdirAll(filepath.Dir(p), 0o755))
	require.NoError(r.t, os.WriteFile(p, []byte(content), 0o600))
}

func newPRTestRepo(t *testing.T, withPRChange bool) *prTestRepo {
	t.Helper()
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	r := &prTestRepo{t: t, dir: t.TempDir()}
	r.git("init", "-q", "-b", "develop")
	r.write("src/A.java", "class A {}")
	r.write("src/B.java", "class B {}")
	r.write("gen/G.java", "class G {}")
	r.write("pom.xml", "<project/>")
	r.write(".ticonfig.yaml", "config:\n  ignore:\n    - gen/**\n")
	r.git("add", ".")
	r.git("commit", "-q", "-m", "base")
	if withPRChange {
		r.git("checkout", "-q", "-b", "feature")
		r.write("src/A.java", "class A {v2}")
		r.write("gen/G.java", "class G {v2}")
		r.write("pom.xml", "<project>v2</project>")
		r.git("add", ".")
		r.git("commit", "-q", "-m", "pr change")
	}
	return r
}

// tiCfgFor builds a ti config for the given branches. Empty branches => manual execution.
func tiCfgFor(url, source, target, sha string) *tiCfg.Cfg {
	cfg := tiCfg.New(url, "token", "acc", "org", "proj", "pipe", "build", "stage", "repo", sha, "",
		source, target, "", "", "", false, "", "")
	return &cfg
}

func Test_getPRChangedFilesForSkip(t *testing.T) {
	log := logrus.New()
	ctx := context.Background()

	t.Run("push trigger does not use PR files", func(t *testing.T) {
		repo := newPRTestRepo(t, true)
		paths, runAll := getPRChangedFilesForSkip(ctx, repo.dir, log, tiCfgFor("http://x", "develop", "develop", "sha"))
		assert.Empty(t, paths)
		assert.False(t, runAll)
	})
	t.Run("manual execution does not use PR files", func(t *testing.T) {
		repo := newPRTestRepo(t, true)
		paths, runAll := getPRChangedFilesForSkip(ctx, repo.dir, log, tiCfgFor("http://x", "", "", ""))
		assert.Empty(t, paths)
		assert.False(t, runAll)
	})
	t.Run("PR returns the changed files", func(t *testing.T) {
		repo := newPRTestRepo(t, true)
		paths, runAll := getPRChangedFilesForSkip(ctx, repo.dir, log, tiCfgFor("http://x", "feature", "develop", "sha"))
		assert.False(t, runAll)
		assert.ElementsMatch(t, []string{"src/A.java", "gen/G.java", "pom.xml"}, paths)
	})
	t.Run("PR with no detectable changes runs all tests", func(t *testing.T) {
		repo := newPRTestRepo(t, false) // single commit: no HEAD@{1} diff
		paths, runAll := getPRChangedFilesForSkip(ctx, repo.dir, log, tiCfgFor("http://x", "feature", "develop", "sha"))
		assert.Empty(t, paths)
		assert.True(t, runAll)
	})
	t.Run("PR where the diff command fails runs all tests", func(t *testing.T) {
		paths, runAll := getPRChangedFilesForSkip(ctx, t.TempDir(), log, tiCfgFor("http://x", "feature", "develop", "sha")) // not a git repo
		assert.Empty(t, paths)
		assert.True(t, runAll)
	})
}

// recordingTI is a stub ti-service that records the skip-tests request and replies with the given skip list.
type recordingTI struct {
	mu    sync.Mutex
	calls []tiClientTypes.SkipTestsRequest
	skip  []string
}

func (r *recordingTI) cfg(t *testing.T, source, target, sha string) *tiCfg.Cfg {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		if req.URL.Path != "/v2/select" {
			_, _ = w.Write([]byte(`{}`))
			return
		}
		var rd io.Reader = req.Body
		if req.Header.Get("Content-Encoding") == "gzip" {
			gz, err := gzip.NewReader(req.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			rd = gz
		}
		body, _ := io.ReadAll(rd)
		var sr tiClientTypes.SkipTestsRequest
		_ = json.Unmarshal(body, &sr)
		r.mu.Lock()
		r.calls = append(r.calls, sr)
		r.mu.Unlock()
		out, _ := json.Marshal(map[string]interface{}{"skipTests": r.skip, "failedTests": []string{}})
		_, _ = w.Write(out)
	}))
	t.Cleanup(srv.Close)
	return tiCfgFor(srv.URL, source, target, sha)
}

func runSkipFlow(t *testing.T, workspace string, cfg *tiCfg.Cfg) (skipFile string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "filter")
	skipFile = filepath.Join(dir, "skip_tests_0")
	err := createSkipAndFailedTestsFiles(context.Background(), filesystem.New(), "step1", workspace, logrus.New(), cfg,
		skipFile, filepath.Join(dir, "failed_tests_0"), map[string]string{}, &api.RunTestsV2Config{IntelligenceMode: true})
	require.NoError(t, err)
	return skipFile
}

func Test_CreateSkipAndFailedTestsFiles_PRChangedFilesGetVirtualChecksum(t *testing.T) {
	repo := newPRTestRepo(t, true)
	real, _, err := instrumentation.GetGitFileChecksums(context.Background(), repo.dir, logrus.New())
	require.NoError(t, err)

	ti := &recordingTI{skip: []string{instrumentation.NonCodeChainPath, "src/test/XTest.java"}}
	skipFile := runSkipFlow(t, repo.dir, ti.cfg(t, "feature", "develop", "sha"))

	require.Len(t, ti.calls, 1)
	sent := ti.calls[0].Files
	assert.NotEqual(t, real["src/A.java"], sent["src/A.java"], "changed file must not carry its real checksum")
	assert.NotEqual(t, real["pom.xml"], sent["pom.xml"], "changed non-code file must not carry its real checksum")
	assert.Equal(t, real["src/B.java"], sent["src/B.java"], "unchanged file keeps its real checksum")
	assert.Equal(t, real["gen/G.java"], sent["gen/G.java"], "ignored file stays ignored")
	assert.Contains(t, readTrimmed(t, skipFile), "src/test/XTest.java")
}

func Test_CreateSkipAndFailedTestsFiles_PushSendsRealChecksums(t *testing.T) {
	repo := newPRTestRepo(t, true)
	real, _, err := instrumentation.GetGitFileChecksums(context.Background(), repo.dir, logrus.New())
	require.NoError(t, err)

	ti := &recordingTI{skip: []string{instrumentation.NonCodeChainPath}}
	runSkipFlow(t, repo.dir, ti.cfg(t, "develop", "develop", "sha"))

	require.Len(t, ti.calls, 1)
	assert.Equal(t, real, ti.calls[0].Files)
}

func Test_CreateSkipAndFailedTestsFiles_ManualSendsRealChecksums(t *testing.T) {
	repo := newPRTestRepo(t, true)
	real, _, err := instrumentation.GetGitFileChecksums(context.Background(), repo.dir, logrus.New())
	require.NoError(t, err)

	ti := &recordingTI{skip: []string{instrumentation.NonCodeChainPath}}
	runSkipFlow(t, repo.dir, ti.cfg(t, "", "", ""))

	require.Len(t, ti.calls, 1)
	assert.Equal(t, real, ti.calls[0].Files)
}

func Test_CreateSkipAndFailedTestsFiles_PRWithoutChangesRunsAllWithoutCallingTI(t *testing.T) {
	repo := newPRTestRepo(t, false)
	ti := &recordingTI{skip: []string{instrumentation.NonCodeChainPath, "src/test/XTest.java"}}

	skipFile := runSkipFlow(t, repo.dir, ti.cfg(t, "feature", "develop", "sha"))

	assert.Empty(t, ti.calls, "ti-service must not be consulted when PR changes are unknown")
	assert.Empty(t, readTrimmed(t, skipFile))
}

func Test_CreateSkipAndFailedTestsFiles_PRDiffFailureStillSplitsForParallelism(t *testing.T) {
	workspace := t.TempDir() // not a git repository: PR diff fails => run all tests
	testDir := filepath.Join(workspace, "src", "test", "java")
	require.NoError(t, os.MkdirAll(testDir, 0o755))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "Test1.java"), []byte("class Test1 {}"), 0o600))
	require.NoError(t, os.WriteFile(filepath.Join(testDir, "Test2.java"), []byte("class Test2 {}"), 0o600))
	ti := &recordingTI{}
	cfg := ti.cfg(t, "feature", "develop", "sha")
	config := &api.RunTestsV2Config{IntelligenceMode: true, TestGlobs: []string{"**/*Test*.java"}}

	skipped := make([]string, 2)
	for idx := range skipped {
		dir := filepath.Join(t.TempDir(), "filter")
		skipFile := filepath.Join(dir, "skip")
		envs := map[string]string{"HARNESS_STEP_INDEX": string(rune('0' + idx)), "HARNESS_STEP_TOTAL": "2"}
		require.NoError(t, createSkipAndFailedTestsFiles(context.Background(), filesystem.New(), "step1", workspace,
			logrus.New(), cfg, skipFile, filepath.Join(dir, "failed"), envs, config))
		skipped[idx] = readTrimmed(t, skipFile)
		assert.NotEmpty(t, skipped[idx])
	}
	assert.NotEqual(t, skipped[0], skipped[1])
	assert.Empty(t, ti.calls)
}
