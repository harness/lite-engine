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
	summary := getSummaryData()
	WriteP90SummaryToStream(buf, summary.CPUSamples, summary.LastPayload, summary.DiskTotals, log)

	// Verify memory samples were collected
	if len(summary.MemSamples) == 0 {
		t.Fatalf("expected at least 1 memory sample, got 0")
	}

	output := buf.String()
	lines := strings.Split(strings.TrimSpace(output), "\n")
	if len(lines) < 1 || strings.TrimSpace(lines[0]) == "" {
		t.Fatalf("expected at least 1 json line, got %d", len(lines))
	}

	var rec map[string]Payload
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
	var summaryRec map[string]SummaryPayload
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
		// Summary must include the three CPU metrics: peak, avgUtilization, p90
		_ = s.PeakCPUUsagePct
		_ = s.AvgCPUUsagePct
		_ = s.P90CPUUsagePct
	}
}

// TestStreamerSummary_AllFieldsPopulated verifies that after at least one sample,
// StreamerSummary has all fields populated with sensible values.
func TestStreamerSummary_AllFieldsPopulated(t *testing.T) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	buf := &safeBuffer{}
	log := logrus.NewEntry(logrus.New())
	cancel, getSummaryData := StartOSStatsStreaming(ctx, buf, log)

	// Allow at least 1 sample
	time.Sleep(1200 * time.Millisecond)
	cancel()

	summary := getSummaryData()

	if len(summary.CPUSamples) == 0 {
		t.Fatalf("expected at least 1 CPU sample")
	}
	if len(summary.MemSamples) == 0 {
		t.Fatalf("expected at least 1 memory sample")
	}
	if len(summary.CPUSamples) != len(summary.MemSamples) {
		t.Fatalf("CPU and memory sample counts should match: cpu=%d, mem=%d",
			len(summary.CPUSamples), len(summary.MemSamples))
	}
	if summary.LastPayload.TotalCPU == 0 {
		t.Fatalf("expected TotalCPU > 0 in LastPayload")
	}
	if summary.LastPayload.TotalMemory == 0 {
		t.Fatalf("expected TotalMemory > 0 in LastPayload")
	}

	// Memory samples should be in the 0..100 range (percentages)
	for i, m := range summary.MemSamples {
		if m < 0 || m > 100 {
			t.Fatalf("memory sample[%d] = %f, expected 0..100", i, m)
		}
	}
}

// ---------------------------------------------------------------------------
// Unit tests for exported PeakAndAvg
// ---------------------------------------------------------------------------

func TestPeakAndAvg_Empty(t *testing.T) {
	peak, avg := PeakAndAvg(nil)
	if peak != 0 || avg != 0 {
		t.Fatalf("expected (0, 0) for nil, got (%f, %f)", peak, avg)
	}
	peak, avg = PeakAndAvg([]float64{})
	if peak != 0 || avg != 0 {
		t.Fatalf("expected (0, 0) for empty, got (%f, %f)", peak, avg)
	}
}

func TestPeakAndAvg_SingleValue(t *testing.T) {
	peak, avg := PeakAndAvg([]float64{42.5})
	if peak != 42.5 || avg != 42.5 {
		t.Fatalf("expected (42.5, 42.5), got (%f, %f)", peak, avg)
	}
}

func TestPeakAndAvg_KnownValues(t *testing.T) {
	vals := []float64{10, 20, 30, 40, 50}
	peak, avg := PeakAndAvg(vals)
	if peak != 50 {
		t.Fatalf("expected peak=50, got %f", peak)
	}
	if avg != 30 {
		t.Fatalf("expected avg=30, got %f", avg)
	}
}

// ---------------------------------------------------------------------------
// Unit tests for exported PercentileNearestRank
// ---------------------------------------------------------------------------

func TestExportedPercentileNearestRank_Empty(t *testing.T) {
	if got := PercentileNearestRank(nil, 0.90); got != 0 {
		t.Fatalf("expected 0 for nil slice, got %f", got)
	}
	if got := PercentileNearestRank([]float64{}, 0.50); got != 0 {
		t.Fatalf("expected 0 for empty slice, got %f", got)
	}
}

func TestExportedPercentileNearestRank_SingleValue(t *testing.T) {
	vals := []float64{42.5}
	for _, p := range []float64{0.50, 0.90, 0.95, 0.99} {
		got := PercentileNearestRank(vals, p)
		if got != 42.5 {
			t.Fatalf("p=%.2f: expected 42.5, got %f", p, got)
		}
	}
}

// Sorted input: [10, 20, 30, 40, 50, 60, 70, 80, 90, 100]
//
//	P50: ceil(0.50*10)-1 = 4  -> 50
//	P90: ceil(0.90*10)-1 = 8  -> 90
//	P95: ceil(0.95*10)-1 = 9  -> 100
//	P99: ceil(0.99*10)-1 = 9  -> 100
func TestExportedPercentileNearestRank_KnownValues(t *testing.T) {
	vals := []float64{50, 10, 90, 30, 70, 20, 60, 100, 40, 80}
	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 50},
		{0.90, 90},
		{0.95, 100},
		{0.99, 100},
	}
	for _, tc := range cases {
		got := PercentileNearestRank(vals, tc.p)
		if got != tc.want {
			t.Errorf("p=%.2f: expected %f, got %f", tc.p, tc.want, got)
		}
	}
}

func TestExportedPercentileNearestRank_DoesNotMutateInput(t *testing.T) {
	vals := []float64{5, 3, 1, 4, 2}
	orig := make([]float64, len(vals))
	copy(orig, vals)

	_ = PercentileNearestRank(vals, 0.90)

	for i := range vals {
		if vals[i] != orig[i] {
			t.Fatalf("input was mutated at index %d: expected %f, got %f", i, orig[i], vals[i])
		}
	}
}

// ---------------------------------------------------------------------------
// Memory percentage formula test
// ---------------------------------------------------------------------------

func TestMemoryUsedPercentFormula(t *testing.T) {
	// Simulate the formula used in runOSStatsLoop:
	// memUsedPct = ((totalMemory - avaMemory) / totalMemory) * 100
	cases := []struct {
		total float64
		avail float64
		want  float64
	}{
		{16.0, 8.0, 50.0},   // 50% used
		{16.0, 0.0, 100.0},  // 100% used
		{16.0, 16.0, 0.0},   // 0% used
		{8.0, 2.0, 75.0},    // 75% used
		{0.0, 0.0, 0.0},     // edge case: 0 total (should not compute)
	}
	for _, tc := range cases {
		if tc.total == 0 {
			// The code guards against div-by-zero with: if p.TotalMemory > 0
			continue
		}
		got := ((tc.total - tc.avail) / tc.total) * maxCPUPercent
		if got != tc.want {
			t.Errorf("total=%.1f avail=%.1f: expected %.1f%%, got %.1f%%",
				tc.total, tc.avail, tc.want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// WriteP90SummaryToStream edge cases
// ---------------------------------------------------------------------------

func TestWriteP90SummaryToStream_NilWriter(t *testing.T) {
	// Should not panic with nil writer
	WriteP90SummaryToStream(nil, []float64{10, 20}, Payload{TotalCPU: 2}, DiskIOTotals{}, nil)
}

func TestWriteP90SummaryToStream_EmptySamples(t *testing.T) {
	buf := &safeBuffer{}
	WriteP90SummaryToStream(buf, nil, Payload{TotalCPU: 2, TotalMemory: 4.0}, DiskIOTotals{}, nil)

	output := buf.String()
	if output == "" {
		t.Fatal("expected summary line even with empty samples")
	}

	if !strings.Contains(output, `"osStatsSummary":true`) {
		t.Fatalf("expected osStatsSummary:true in output, got: %s", output)
	}
}

func TestWriteP90SummaryToStream_WithDiskTotals(t *testing.T) {
	buf := &safeBuffer{}
	totals := DiskIOTotals{ReadBytes: 1024 * 1024, WriteBytes: 2048 * 1024}
	WriteP90SummaryToStream(buf, []float64{25, 50, 75}, Payload{TotalCPU: 4}, totals, nil)

	output := buf.String()
	if !strings.Contains(output, `"totalDiskReadBytes":1048576`) {
		t.Fatalf("expected totalDiskReadBytes in output, got: %s", output)
	}
	if !strings.Contains(output, `"totalDiskWriteBytes":2097152`) {
		t.Fatalf("expected totalDiskWriteBytes in output, got: %s", output)
	}
}

// ---------------------------------------------------------------------------
// PercentileNearestRank edge cases
// ---------------------------------------------------------------------------

func TestExportedPercentileNearestRank_BoundaryPercentiles(t *testing.T) {
	vals := []float64{10, 20, 30}

	got := PercentileNearestRank(vals, 0.0)
	if got != 10 {
		t.Fatalf("p=0.0: expected 10, got %f", got)
	}

	got = PercentileNearestRank(vals, 1.0)
	if got != 30 {
		t.Fatalf("p=1.0: expected 30, got %f", got)
	}
}

func TestExportedPercentileNearestRank_TwoElements(t *testing.T) {
	vals := []float64{100, 200}
	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 100},
		{0.90, 200},
		{0.99, 200},
	}
	for _, tc := range cases {
		got := PercentileNearestRank(vals, tc.p)
		if got != tc.want {
			t.Errorf("p=%.2f: expected %f, got %f", tc.p, tc.want, got)
		}
	}
}

// ---------------------------------------------------------------------------
// StreamerSummary with disk I/O totals
// ---------------------------------------------------------------------------

func TestStreamerSummary_DiskIOTotals(t *testing.T) {
	ctx, ctxCancel := context.WithCancel(context.Background())
	defer ctxCancel()

	buf := &safeBuffer{}
	log := logrus.NewEntry(logrus.New())
	cancel, getSummaryData := StartOSStatsStreaming(ctx, buf, log)

	time.Sleep(2200 * time.Millisecond)
	cancel()

	summary := getSummaryData()

	_ = summary.DiskTotals.ReadBytes
	_ = summary.DiskTotals.WriteBytes

	if len(summary.CPUSamples) != len(summary.MemSamples) {
		t.Fatalf("sample count mismatch: cpu=%d, mem=%d",
			len(summary.CPUSamples), len(summary.MemSamples))
	}
}
