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

	state, reports, duration, err := ParseSavings(workspace, logrus.New())
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
	state, reports, duration, err := ParseSavings(t.TempDir(), logrus.New())
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
