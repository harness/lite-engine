package osstats

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/sirupsen/logrus"
)

// safeBuffer is a thread-safe buffer for testing.
type safeBuffer struct {
	buf bytes.Buffer
	mu  sync.Mutex
}

func (sb *safeBuffer) Write(p []byte) (n int, err error) {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.Write(p)
}

func (sb *safeBuffer) String() string {
	sb.mu.Lock()
	defer sb.mu.Unlock()
	return sb.buf.String()
}

func TestStartOSStatsStreaming_WritesNDJSON(t *testing.T) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	buf := &safeBuffer{}

	// Start streaming to the buffer
	cancel := StartOSStatsStreaming(ctx, buf, logrus.NewEntry(logrus.New()))

	// Allow at least 1 sample (cpu.Percent waits ~1s)
	time.Sleep(1200 * time.Millisecond)

	// Stop the streaming
	cancel()

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected at least 1 json line, got %d", len(lines))
	}

	var rec map[string]OSStatsPayload
	if err := json.Unmarshal([]byte(lines[0]), &rec); err != nil {
		t.Fatalf("unmarshal first line: %v; line=%q", err, lines[0])
	}
	if len(rec) != 1 {
		t.Fatalf("expected exactly 1 timestamp key, got %d", len(rec))
	}

	// The final NDJSON line should be a summary record with p90 CPU usage.
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine == "" {
		t.Fatalf("expected non-empty last line")
	}

	var summary map[string]map[string]interface{}
	if err := json.Unmarshal([]byte(lastLine), &summary); err != nil {
		t.Fatalf("unmarshal last line: %v; line=%q", err, lastLine)
	}
	if len(summary) != 1 {
		t.Fatalf("expected exactly 1 timestamp key in last line, got %d", len(summary))
	}
	for _, payload := range summary {
		if _, ok := payload["p90CPUUsagePct"]; !ok {
			t.Fatalf("expected last line to contain p90CPUUsagePct; payload=%v", payload)
		}
	}
}
