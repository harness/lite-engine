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
	log := logrus.NewEntry(logrus.New())
	cancel, getSummaryData := StartOSStatsStreaming(ctx, buf, log)

	// Allow at least 1 sample (cpu.Percent waits ~1s)
	time.Sleep(1200 * time.Millisecond)

	// Stop the streaming
	cancel()

	// Write P90 summary to stream before "closing" (same as handler does)
	samples, lastPayload := getSummaryData()
	WriteP90SummaryToStream(buf, samples, lastPayload, log)

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
	for _, payload := range rec {
		if payload.TotalMemory == 0 && payload.AvaMemory == 0 {
			t.Fatalf("expected memory fields in payload; got %+v", payload)
		}
		if payload.TotalCPU == 0 {
			t.Fatalf("expected totalCPU in payload; got %+v", payload)
		}
	}

	// Last line must be the P90 summary with totalCPU and same memory/disk metrics
	lastLine := strings.TrimSpace(lines[len(lines)-1])
	if lastLine == "" {
		t.Fatalf("expected non-empty last line (summary)")
	}
	var summaryRec map[string]OSStatsSummaryPayload
	if err := json.Unmarshal([]byte(lastLine), &summaryRec); err != nil {
		t.Fatalf("unmarshal last line (summary): %v; line=%q", err, lastLine)
	}
	if len(summaryRec) != 1 {
		t.Fatalf("expected exactly 1 timestamp key in summary, got %d", len(summaryRec))
	}
	for _, s := range summaryRec {
		if s.TotalCPU == 0 {
			t.Fatalf("expected totalCPU in summary; got %+v", s)
		}
		// p90CPUUsagePct is always present (0 if no samples)
		_ = s.P90CPUUsagePct
	}
}
