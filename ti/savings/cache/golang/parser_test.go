package golang

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/harness/ti-client/types"
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
