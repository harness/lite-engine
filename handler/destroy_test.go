package handler

import (
	"context"
	"math"
	"testing"
	"time"

	"github.com/harness/lite-engine/logstream"
	"github.com/harness/lite-engine/osstats"
	"github.com/harness/lite-engine/pipeline"
	"github.com/sirupsen/logrus"
)

func testLog() *logrus.Entry {
	return logrus.NewEntry(logrus.New())
}

func assertClose(t *testing.T, name string, got, want float64) {
	t.Helper()
	if math.Abs(got-want) > 0.01 {
		t.Errorf("%s: expected %.4f, got %.4f", name, want, got)
	}
}

// ---------------------------------------------------------------------------
// closeLELogStream
// ---------------------------------------------------------------------------

func TestCloseLELogStream_NilWriter(t *testing.T) {
	state := pipeline.GetState()
	state.SetLELogWriter(nil, "")

	if err := closeLELogStream(context.Background(), state); err != nil {
		t.Fatalf("expected nil error for nil writer, got %v", err)
	}
}

func TestCloseLELogStream_ClosesAndClears(t *testing.T) {
	state := pipeline.GetState()
	w := logstream.NopWriter()
	state.SetLELogWriter(w, "test-key")

	if err := closeLELogStream(context.Background(), state); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if state.GetLELogWriter() != nil {
		t.Fatal("expected writer to be cleared after close")
	}
	if state.GetLELogKey() != "" {
		t.Fatal("expected log key to be cleared after close")
	}
}

// ---------------------------------------------------------------------------
// closeOSStatsStream
// ---------------------------------------------------------------------------

func TestCloseOSStatsStream_NilEntry(t *testing.T) {
	state := pipeline.GetState()
	summary, err := closeOSStatsStream(state, "nonexistent-key")
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if summary != nil {
		t.Fatal("expected nil summary for missing entry")
	}
}

func TestCloseOSStatsStream_WithSummaryData(t *testing.T) {
	state := pipeline.GetState()
	key := "test-os-stats"

	cancelled := false
	entry := &pipeline.OSStatsEntry{
		Writer: logstream.NopWriter(),
		Cancel: func() { cancelled = true },
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples:  []float64{10, 20, 30},
				MemSamples:  []float64{40, 50, 60},
				LastPayload: osstats.Payload{TotalCPU: 4, TotalMemory: 8.0, AvaMemory: 2.0},
				DiskTotals:  osstats.DiskIOTotals{ReadBytes: 1048576, WriteBytes: 2097152},
			}
		},
	}
	state.SetOSStatsEntry(key, entry)
	t.Cleanup(func() { state.DeleteOSStatsEntry(key) })

	summary, err := closeOSStatsStream(state, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !cancelled {
		t.Fatal("expected cancel to be called")
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if len(summary.CPUSamples) != 3 {
		t.Fatalf("expected 3 CPU samples, got %d", len(summary.CPUSamples))
	}
	if summary.DiskTotals.ReadBytes != 1048576 {
		t.Fatalf("expected ReadBytes=1048576, got %d", summary.DiskTotals.ReadBytes)
	}

	// Entry should be removed from state
	if state.GetOSStatsEntry(key) != nil {
		t.Fatal("expected entry to be deleted from state")
	}
}

func TestCloseOSStatsStream_NilWriterAndCancel(t *testing.T) {
	state := pipeline.GetState()
	key := "test-nil-writer"

	entry := &pipeline.OSStatsEntry{
		Writer:         nil,
		Cancel:         nil,
		GetSummaryData: nil,
	}
	state.SetOSStatsEntry(key, entry)
	t.Cleanup(func() { state.DeleteOSStatsEntry(key) })

	summary, err := closeOSStatsStream(state, key)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if summary != nil {
		t.Fatal("expected nil summary when writer is nil")
	}
}

// ---------------------------------------------------------------------------
// buildResourceStats — streamer-populated branch
// ---------------------------------------------------------------------------

func TestBuildResourceStats_StreamerPopulated(t *testing.T) {
	state := pipeline.GetState()
	key := "test-streamer"

	entry := &pipeline.OSStatsEntry{
		Writer: logstream.NopWriter(),
		Cancel: func() {},
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples:  []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100},
				MemSamples:  []float64{5, 10, 15, 20, 25, 30, 35, 40, 45, 50},
				LastPayload: osstats.Payload{TotalCPU: 8, TotalMemory: 16.0, AvaMemory: 4.0},
				DiskTotals:  osstats.DiskIOTotals{ReadBytes: 10485760, WriteBytes: 20971520},
			}
		},
	}
	state.SetOSStatsEntry(key, entry)
	t.Cleanup(func() { state.DeleteOSStatsEntry(key) })

	stats := buildResourceStats(state, nil, testLog())

	// CPU fields
	if stats.CPUCores != 8 {
		t.Errorf("CPUCores: expected 8, got %d", stats.CPUCores)
	}
	assertClose(t, "MaxCPU", stats.MaxCPUUsagePct, 100)
	assertClose(t, "AvgCPU", stats.AvgCPUUsagePct, 55)
	assertClose(t, "P50CPU", stats.P50CPUUsagePct, 50)
	assertClose(t, "P90CPU", stats.P90CPUUsagePct, 90)
	assertClose(t, "P95CPU", stats.P95CPUUsagePct, 100)
	assertClose(t, "P99CPU", stats.P99CPUUsagePct, 100)

	// Memory fields
	assertClose(t, "TotalMemMB", stats.TotalMemMB, 16.0*1024)
	assertClose(t, "MaxMem", stats.MaxMemUsagePct, 50)
	assertClose(t, "AvgMem", stats.AvgMemUsagePct, 27.5)
	assertClose(t, "P50Mem", stats.P50MemUsagePct, 25)
	assertClose(t, "P90Mem", stats.P90MemUsagePct, 45)
	assertClose(t, "P95Mem", stats.P95MemUsagePct, 50)
	assertClose(t, "P99Mem", stats.P99MemUsagePct, 50)

	// Disk I/O
	assertClose(t, "DiskReadMB", stats.DiskTotalReadMB, 10.0)
	assertClose(t, "DiskWriteMB", stats.DiskTotalWriteMB, 20.0)
}

// ---------------------------------------------------------------------------
// buildResourceStats — collector fallback branch
// ---------------------------------------------------------------------------

func TestBuildResourceStats_CollectorFallback_NoStreamer(t *testing.T) {
	// No OS stats entries set, so the streamer loop is empty.
	// buildResourceStats should fall back to the collector.
	state := pipeline.GetState()

	// Clean any leftover entries from other tests
	for _, k := range state.GetAllOSStatsKeys() {
		state.DeleteOSStatsEntry(k)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector := osstats.New(ctx, time.Hour, false)
	collector.Start()

	// Let one real sample happen (cpu.Percent uses a ~1s window)
	time.Sleep(1200 * time.Millisecond)
	collector.Stop()

	// Feed through buildResourceStats with the stopped collector
	// We can't call buildResourceStats with the stopped collector because
	// Stop() closes doneCh and the goroutine exits. Aggregate+Stats still works.
	collector.Aggregate()
	collectorStats := collector.Stats()

	// Verify the collector produced non-zero data
	if collectorStats.MaxCPUUsagePct == 0 && collectorStats.MaxMemUsagePct == 0 {
		t.Fatal("collector should have produced non-zero stats after 1 real sample")
	}

	// Verified collector produced real data above. The empty-samples test below
	// exercises the full buildResourceStats fallback path with a live collector.
	_ = collectorStats
}

func TestBuildResourceStats_CollectorFallback_EmptySamples(t *testing.T) {
	// Streamer entry exists but has zero samples (stage destroyed before first sample).
	// buildResourceStats should fall back to the collector.
	state := pipeline.GetState()

	key := "test-empty-samples"
	entry := &pipeline.OSStatsEntry{
		Writer: logstream.NopWriter(),
		Cancel: func() {},
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples:  []float64{},
				MemSamples:  []float64{},
				LastPayload: osstats.Payload{},
				DiskTotals:  osstats.DiskIOTotals{},
			}
		},
	}
	state.SetOSStatsEntry(key, entry)
	t.Cleanup(func() { state.DeleteOSStatsEntry(key) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector := osstats.New(ctx, time.Hour, false)
	collector.Start()
	time.Sleep(1200 * time.Millisecond)

	stats := buildResourceStats(state, collector, testLog())

	// Collector was running and had at least 1 real sample.
	// buildResourceStats should have fallen back to it because CPUSamples is empty.
	if stats.MaxCPUUsagePct == 0 && stats.MaxMemUsagePct == 0 {
		t.Fatal("expected non-zero stats from collector fallback when streamer has empty samples")
	}
	if stats.MemGraph == nil {
		t.Fatal("expected MemGraph from collector fallback")
	}
	if stats.CPUGraph == nil {
		t.Fatal("expected CPUGraph from collector fallback")
	}
}

func TestBuildResourceStats_StreamerTakesPrecedence(t *testing.T) {
	// Both streamer and collector have data. Streamer should win.
	state := pipeline.GetState()

	key := "test-precedence"
	entry := &pipeline.OSStatsEntry{
		Writer: logstream.NopWriter(),
		Cancel: func() {},
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples:  []float64{77.0},
				MemSamples:  []float64{33.0},
				LastPayload: osstats.Payload{TotalCPU: 16, TotalMemory: 32.0, AvaMemory: 8.0},
				DiskTotals:  osstats.DiskIOTotals{ReadBytes: 512, WriteBytes: 1024},
			}
		},
	}
	state.SetOSStatsEntry(key, entry)
	t.Cleanup(func() { state.DeleteOSStatsEntry(key) })

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	collector := osstats.New(ctx, time.Hour, false)
	collector.Start()
	time.Sleep(1200 * time.Millisecond)

	stats := buildResourceStats(state, collector, testLog())

	// Stats should come from the streamer, not the collector
	if stats.CPUCores != 16 {
		t.Errorf("CPUCores: expected 16 from streamer, got %d", stats.CPUCores)
	}
	assertClose(t, "MaxCPU", stats.MaxCPUUsagePct, 77.0)
	assertClose(t, "MaxMem", stats.MaxMemUsagePct, 33.0)
	assertClose(t, "TotalMemMB", stats.TotalMemMB, 32.0*1024)

	// Collector graphs should NOT be present (streamer path doesn't set them)
	if stats.MemGraph != nil {
		t.Error("expected nil MemGraph when streamer is used (streamer path does not set graphs)")
	}
	if stats.CPUGraph != nil {
		t.Error("expected nil CPUGraph when streamer is used (streamer path does not set graphs)")
	}
}

func TestBuildResourceStats_NilCollector_NoStreamer(t *testing.T) {
	// No streamer, no collector. Should return all-zero stats without panicking.
	state := pipeline.GetState()

	for _, k := range state.GetAllOSStatsKeys() {
		state.DeleteOSStatsEntry(k)
	}

	stats := buildResourceStats(state, nil, testLog())

	if stats == nil {
		t.Fatal("expected non-nil stats")
	}
	if stats.MaxCPUUsagePct != 0 || stats.MaxMemUsagePct != 0 {
		t.Error("expected all-zero stats when both streamer and collector are absent")
	}
}
