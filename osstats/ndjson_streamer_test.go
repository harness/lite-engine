package osstats

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

func TestNDJSONStreamer_WritesNDJSON(t *testing.T) {
	tmp, err := os.CreateTemp("", "le-osstats-*.ndjson")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	path := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(path)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	s, err := NewNDJSONStreamer(ctx, path, logrus.NewEntry(logrus.New()))
	if err != nil {
		t.Fatalf("NewNDJSONStreamer: %v", err)
	}

	s.Start()
	time.Sleep(1200 * time.Millisecond) // allow at least 1 sample (cpu.Percent waits ~1s)
	s.Stop()

	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}

	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	if len(lines) < 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected at least 1 json line, got %d", len(lines))
	}

	var rec map[string]osStatsPayload
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal first line: %v; line=%q", err, lines[0])
	}
	if len(rec) != 1 {
		t.Fatalf("expected exactly 1 timestamp key, got %d", len(rec))
	}
}

