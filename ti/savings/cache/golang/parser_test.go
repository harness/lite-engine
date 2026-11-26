package golang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/harness/ti-client/types"
	golangTypes "github.com/harness/ti-client/types/cache/golang"
	"github.com/sirupsen/logrus"
)

func TestParseSavings_Optimized(t *testing.T) {
	workspace := t.TempDir()
	reportPath := filepath.Join(workspace, ".harness", "go-cache-report.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{
		"version":            1,
		"mode":               "bundled",
		"gets":               10,
		"hits":               8,
		"misses":             2,
		"puts":               1,
		"bytes_restored":     1000,
		"bytes_stored":       100,
		"duration_ms":        1500,
		"started_at_unix_ms": 1,
		"ended_at_unix_ms":   2,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	state, reports, duration, err := ParseSavings(workspace, logrus.New(), nil)
	if err != nil {
		t.Fatalf("ParseSavings error: %v", err)
	}
	if state != types.OPTIMIZED {
		t.Fatalf("state = %s, want OPTIMIZED", state)
	}
	if len(reports) != 1 || reports[0].Hits != 8 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
	if duration != 1500 {
		t.Fatalf("duration = %d, want 1500", duration)
	}
}

func TestParseSavings_DisabledWhenMissing(t *testing.T) {
	state, reports, duration, err := ParseSavings(t.TempDir(), logrus.New(), nil)
	if err == nil {
		t.Fatal("expected error when report missing")
	}
	if state != types.DISABLED {
		t.Fatalf("state = %s, want DISABLED", state)
	}
	if len(reports) != 0 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
	if duration != 0 {
		t.Fatalf("duration = %d, want 0", duration)
	}
}

func TestIsolateSharedTmpForLocalInfra(t *testing.T) {
	t.Setenv("HARNESS_EXECUTION_ID", "exec-abc")
	if got, want := isolateSharedTmp("/tmp/"), "/tmp/harness/exec-abc"; got != want {
		t.Fatalf("isolateSharedTmp(/tmp/) = %q, want %q", got, want)
	}
	if got := isolateSharedTmp("/addon/tmp"); got != "/addon/tmp" {
		t.Fatalf("isolateSharedTmp(/addon/tmp) = %q, want /addon/tmp", got)
	}
}

// TestParseSavings_CloudVMStepEnvsFindsIsolatedReport reproduces the Cloud VM
// failure mode: go-cache-proxy writes under /tmp/harness/<exec>/ using step env,
// while the lite-engine process itself does not have HARNESS_TMP_PATH set.
func TestParseSavings_CloudVMStepEnvsFindsIsolatedReport(t *testing.T) {
	t.Setenv("HARNESS_TMP_PATH", "")
	t.Setenv("HARNESS_EXECUTION_ID", "")
	t.Setenv("HARNESS_GO_CACHE_REPORT_PATH", "")
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return filepath.Join(t.TempDir(), "missing-engine") }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	execID := "ci18519-go-savings-ut"
	reportDir := "/tmp/harness/" + execID
	if err := os.MkdirAll(reportDir, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(reportDir) })

	reportPath := filepath.Join(reportDir, "go-cache-report.json")
	payload := map[string]any{
		"version":     1,
		"gets":        4,
		"hits":        3,
		"misses":      1,
		"puts":        0,
		"duration_ms": 900,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	// Without step envs (and with empty process env), report is not found.
	if _, _, _, err := ParseSavings(t.TempDir(), logrus.New(), nil); err == nil {
		t.Fatal("expected miss when process env and step envs are both empty")
	}

	// Step envs alone are enough — mirrors ParseAndUploadSavings(r.Envs).
	stepEnvs := map[string]string{
		"HARNESS_TMP_PATH":     "/tmp/",
		"HARNESS_EXECUTION_ID": execID,
	}
	state, reports, duration, err := ParseSavings(t.TempDir(), logrus.New(), stepEnvs)
	if err != nil {
		t.Fatalf("ParseSavings with step envs error: %v", err)
	}
	if state != types.OPTIMIZED {
		t.Fatalf("state = %s, want OPTIMIZED", state)
	}
	if len(reports) != 1 || reports[0].Hits != 3 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
	if duration != 900 {
		t.Fatalf("duration = %d, want 900", duration)
	}
}

func TestIsolateSharedTmpWithEnvs_PrefersStepExecutionID(t *testing.T) {
	t.Setenv("HARNESS_EXECUTION_ID", "process-exec-id")
	got := isolateSharedTmpWithEnvs("/tmp/", map[string]string{
		"HARNESS_EXECUTION_ID": "step-exec-id",
	})
	want := "/tmp/harness/step-exec-id"
	if got != want {
		t.Fatalf("isolateSharedTmpWithEnvs = %q, want %q", got, want)
	}
}

func TestEnvValue_PrefersStepEnvs(t *testing.T) {
	t.Setenv("HARNESS_TMP_PATH", "/from-process")
	if got := envValue(map[string]string{"HARNESS_TMP_PATH": "/from-step"}, "HARNESS_TMP_PATH"); got != "/from-step" {
		t.Fatalf("got %q, want /from-step", got)
	}
	if got := envValue(nil, "HARNESS_TMP_PATH"); got != "/from-process" {
		t.Fatalf("got %q, want /from-process", got)
	}
	if got := envValue(map[string]string{"HARNESS_TMP_PATH": "  "}, "HARNESS_TMP_PATH"); got != "/from-process" {
		t.Fatalf("blank step env should fall back, got %q", got)
	}
}

func TestDefaultSharedEngineVolPath(t *testing.T) {
	t.Setenv("HARNESS_WORKDIR", "")
	if got := defaultSharedEngineVolPath(nil); got != "/tmp/engine" {
		t.Fatalf("defaultSharedEngineVolPath(nil) = %q, want /tmp/engine", got)
	}
	envs := map[string]string{"HARNESS_WORKDIR": "/ws"}
	want := filepath.Join("/ws", "tmp", "engine")
	if got := defaultSharedEngineVolPath(envs); got != want {
		t.Fatalf("defaultSharedEngineVolPath(envs) = %q, want %q", got, want)
	}
}

// TestParseSavings_SharedEngineVolFindsReport covers CI-25598: go-cache-proxy
// writes under /tmp/engine (LE shared vol) so containerized hosted/VM steps
// remain visible to host-side lite-engine after the step exits.
func TestParseSavings_SharedEngineVolFindsReport(t *testing.T) {
	t.Setenv("HARNESS_TMP_PATH", "")
	t.Setenv("HARNESS_EXECUTION_ID", "")
	t.Setenv("HARNESS_GO_CACHE_REPORT_PATH", "")

	engine := t.TempDir()
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return engine }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	reportPath := filepath.Join(engine, "go-cache-report.json")
	payload := map[string]any{
		"version":     1,
		"gets":        5,
		"hits":        4,
		"misses":      1,
		"puts":        0,
		"duration_ms": 700,
	}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}

	state, reports, duration, err := ParseSavings(t.TempDir(), logrus.New(), nil)
	if err != nil {
		t.Fatalf("ParseSavings error: %v", err)
	}
	if state != types.OPTIMIZED {
		t.Fatalf("state = %s, want OPTIMIZED", state)
	}
	if len(reports) != 1 || reports[0].Hits != 4 {
		t.Fatalf("unexpected reports: %+v", reports)
	}
	if duration != 700 {
		t.Fatalf("duration = %d, want 700", duration)
	}
	if _, err := os.Stat(reportPath); !os.IsNotExist(err) {
		t.Fatalf("expected report to be removed after parse, err=%v", err)
	}
}

func TestParseSavings_FullRunWhenNoHits(t *testing.T) {
	workspace := t.TempDir()
	reportPath := filepath.Join(workspace, ".harness", "go-cache-report.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		t.Fatal(err)
	}
	payload := map[string]any{"version": 1, "gets": 3, "hits": 0, "misses": 3, "puts": 2, "duration_ms": 100}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, data, 0o600); err != nil {
		t.Fatal(err)
	}
	state, _, _, err := ParseSavings(workspace, logrus.New(), nil)
	if err != nil {
		t.Fatalf("ParseSavings error: %v", err)
	}
	if state != types.FULL_RUN {
		t.Fatalf("state = %s, want FULL_RUN", state)
	}
}

func TestParseSavings_SkipsInvalidJSON(t *testing.T) {
	workspace := t.TempDir()
	reportPath := filepath.Join(workspace, ".harness", "go-cache-report.json")
	if err := os.MkdirAll(filepath.Dir(reportPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(reportPath, []byte("not-json"), 0o600); err != nil {
		t.Fatal(err)
	}
	state, reports, _, err := ParseSavings(workspace, logrus.New(), nil)
	if err == nil {
		t.Fatal("expected error when only invalid report exists")
	}
	if state != types.DISABLED || len(reports) != 0 {
		t.Fatalf("state=%s reports=%d", state, len(reports))
	}
}

func TestParseSavings_ExplicitReportPathAndWorkdir(t *testing.T) {
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return filepath.Join(t.TempDir(), "missing") }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	explicit := filepath.Join(t.TempDir(), "explicit.json")
	payload := map[string]any{"version": 0, "gets": 1, "hits": 1, "duration_ms": 10}
	data, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(explicit, data, 0o600); err != nil {
		t.Fatal(err)
	}
	envs := map[string]string{"HARNESS_GO_CACHE_REPORT_PATH": explicit}
	state, reports, _, err := ParseSavings(t.TempDir(), logrus.New(), envs)
	if err != nil {
		t.Fatalf("ParseSavings error: %v", err)
	}
	if state != types.OPTIMIZED || len(reports) != 1 || reports[0].Version != 1 {
		t.Fatalf("unexpected result state=%s reports=%+v", state, reports)
	}

	workdir := t.TempDir()
	wdReport := filepath.Join(workdir, ".harness", "go-cache-report.json")
	if err := os.MkdirAll(filepath.Dir(wdReport), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(wdReport, data, 0o600); err != nil {
		t.Fatal(err)
	}
	state, _, _, err = ParseSavings(t.TempDir(), logrus.New(), map[string]string{"HARNESS_WORKDIR": workdir})
	if err != nil || state != types.OPTIMIZED {
		t.Fatalf("workdir report not found: state=%s err=%v", state, err)
	}
}

func TestGetMetadataFromGoMetrics(t *testing.T) {
	gets, hits := GetMetadataFromGoMetrics(nil)
	if gets != 0 || hits != 0 {
		t.Fatalf("nil metrics: gets=%d hits=%d", gets, hits)
	}
	req := &types.SavingsRequest{}
	req.GoMetrics.Reports = []golangTypes.Report{
		{Gets: 4, Hits: 3},
		{Gets: 2, Hits: 1},
	}
	gets, hits = GetMetadataFromGoMetrics(req)
	if gets != 6 || hits != 4 {
		t.Fatalf("GetMetadataFromGoMetrics = (%d,%d), want (6,4)", gets, hits)
	}
}

func TestExecutionScopeAndSanitize(t *testing.T) {
	t.Setenv("HARNESS_EXECUTION_ID", "")
	t.Setenv("HARNESS_STAGE_ID", "")
	t.Setenv("HARNESS_BUILD_ID", "")
	if got := executionScopeWithEnvs(nil); got != "unknown" {
		t.Fatalf("executionScopeWithEnvs = %q, want unknown", got)
	}
	if sanitizePathElement("  ") != "" {
		t.Fatal("expected blank sanitize to return empty")
	}
}

func TestParseReportFile_DirectoryError(t *testing.T) {
	dir := t.TempDir()
	reportPath := filepath.Join(dir, "go-cache-report.json")
	if err := os.Mkdir(reportPath, 0755); err != nil {
		t.Fatal(err)
	}
	if _, err := parseReportFile(reportPath); err == nil {
		t.Fatal("expected error reading directory as report file")
	}
}

func TestFindReportFiles_DedupesPaths(t *testing.T) {
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return filepath.Join(t.TempDir(), "missing") }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "go-cache-report.json")
	if err := os.WriteFile(reportPath, []byte(`{"version":1,"gets":1,"hits":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Same path via explicit env and as workspace/.harness would differ; feed explicit twice via env+same file.
	envs := map[string]string{"HARNESS_GO_CACHE_REPORT_PATH": reportPath}
	files := findReportFiles(dir, envs)
	if len(files) != 1 {
		t.Fatalf("expected deduped single candidate, got %v", files)
	}
}

func TestParseSavings_RemoveFailureIsNonFatal(t *testing.T) {
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return filepath.Join(t.TempDir(), "missing") }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	dir := t.TempDir()
	reportPath := filepath.Join(dir, "go-cache-report.json")
	if err := os.WriteFile(reportPath, []byte(`{"version":1,"gets":1,"hits":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	// Make the parent directory read-only so os.Remove fails (non-IsNotExist).
	// Root (common in CI) can still unlink; probe and skip in that case.
	if err := os.Chmod(dir, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	if err := os.Remove(reportPath); err == nil {
		t.Skip("os.Remove succeeded on 0555 dir (likely root); cannot exercise remove-failure path")
	}

	state, reports, _, err := ParseSavings(t.TempDir(), logrus.New(), map[string]string{
		"HARNESS_GO_CACHE_REPORT_PATH": reportPath,
	})
	if err != nil {
		t.Fatalf("ParseSavings error: %v", err)
	}
	if state != types.OPTIMIZED || len(reports) != 1 {
		t.Fatalf("state=%s reports=%d", state, len(reports))
	}
	// File should still exist because remove failed.
	if _, err := os.Stat(reportPath); err != nil {
		t.Fatalf("expected report to remain after failed remove: %v", err)
	}
}

func TestFindReportFiles_EmptyExplicitSkipped(t *testing.T) {
	orig := sharedEngineVolPath
	sharedEngineVolPath = func(map[string]string) string { return "" }
	t.Cleanup(func() { sharedEngineVolPath = orig })

	files := findReportFiles(t.TempDir(), map[string]string{"HARNESS_GO_CACHE_REPORT_PATH": ""})
	if len(files) != 0 {
		t.Fatalf("expected no candidates, got %v", files)
	}
}
