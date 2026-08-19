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
	data, _ := json.Marshal(payload)
	if err := os.WriteFile(reportPath, data, 0644); err != nil {
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
	_, _, _, err := ParseSavings(t.TempDir(), logrus.New())
	if err == nil {
		t.Fatal("expected error when report missing")
	}
}
