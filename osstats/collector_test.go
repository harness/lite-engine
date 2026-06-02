package osstats

import (
	"context"
	"math"
	"testing"
	"time"
)

func nowForTest() time.Time { return time.Now() }

func newTestCollector() *StatsCollector {
	c := New(context.Background(), time.Second, false)
	c.st = nowForTest()
	return c
}

func TestPercentileNearestRank_Empty(t *testing.T) {
	if got := percentileNearestRank(nil, 0.95); got != 0 {
		t.Fatalf("expected 0 for nil samples, got %v", got)
	}
	if got := percentileNearestRank([]float64{}, 0.95); got != 0 {
		t.Fatalf("expected 0 for empty samples, got %v", got)
	}
}

func TestPercentileNearestRank_SingleValue(t *testing.T) {
	for _, p := range []float64{0.5, 0.9, 0.95, 0.99} {
		if got := percentileNearestRank([]float64{42}, p); got != 42 {
			t.Fatalf("p=%v: expected 42, got %v", p, got)
		}
	}
}

func TestPercentileNearestRank_KnownDistribution(t *testing.T) {
	// 1..100 sorted. Nearest-rank: idx = ceil(p*N) - 1.
	values := make([]float64, 100)
	for i := range values {
		values[i] = float64(i + 1)
	}
	cases := []struct {
		p    float64
		want float64
	}{
		{0.50, 50},
		{0.90, 90},
		{0.95, 95},
		{0.99, 99},
	}
	for _, c := range cases {
		got := percentileNearestRank(values, c.p)
		if got != c.want {
			t.Fatalf("p=%v: expected %v, got %v", c.p, c.want, got)
		}
	}
}

func TestPercentileNearestRank_DoesNotMutateInput(t *testing.T) {
	values := []float64{5, 1, 4, 2, 3}
	original := append([]float64{}, values...)
	_ = percentileNearestRank(values, 0.95)
	for i := range values {
		if values[i] != original[i] {
			t.Fatalf("input mutated at idx %d: got %v want %v", i, values[i], original[i])
		}
	}
}

func TestPercentileNearestRank_UnsortedInput(t *testing.T) {
	values := []float64{50, 10, 90, 30, 70, 20, 80, 40, 100, 60}
	got := percentileNearestRank(values, 0.90)
	want := 90.0
	if got != want {
		t.Fatalf("expected %v for p90 of unsorted, got %v", want, got)
	}
}

func TestPercentileNearestRank_OutOfRangeP(t *testing.T) {
	values := []float64{1, 2, 3, 4, 5}
	// p > 1 should clamp to last index
	if got := percentileNearestRank(values, 1.5); got != 5 {
		t.Fatalf("expected 5 for p=1.5 (clamped), got %v", got)
	}
	// p <= 0 should clamp to first index
	if got := percentileNearestRank(values, 0); got != 1 {
		t.Fatalf("expected 1 for p=0 (clamped), got %v", got)
	}
}

func TestUpdate_TracksDiskAndSamples(t *testing.T) {
	c := newTestCollector()

	stats := []osStat{
		{CPUPct: 10, MemPct: 20, DiskPct: 30},
		{CPUPct: 50, MemPct: 60, DiskPct: 70},
		{CPUPct: 90, MemPct: 80, DiskPct: 40},
	}
	for i := range stats {
		c.update(&stats[i])
	}

	if got, want := len(c.cpuSamples), 3; got != want {
		t.Fatalf("cpuSamples len: got %d want %d", got, want)
	}
	if got, want := len(c.memSamples), 3; got != want {
		t.Fatalf("memSamples len: got %d want %d", got, want)
	}
	if got, want := len(c.diskSamples), 3; got != want {
		t.Fatalf("diskSamples len: got %d want %d", got, want)
	}

	if c.stats.MaxCPUUsagePct != 90 {
		t.Fatalf("MaxCPUUsagePct: got %v want 90", c.stats.MaxCPUUsagePct)
	}
	if c.stats.MaxMemUsagePct != 80 {
		t.Fatalf("MaxMemUsagePct: got %v want 80", c.stats.MaxMemUsagePct)
	}
	if c.stats.PeakDiskUsagePct != 70 {
		t.Fatalf("PeakDiskUsagePct: got %v want 70", c.stats.PeakDiskUsagePct)
	}

	if !approxEq(c.cpuPctSum, 150) {
		t.Fatalf("cpuPctSum: got %v want 150", c.cpuPctSum)
	}
	if !approxEq(c.memPctSum, 160) {
		t.Fatalf("memPctSum: got %v want 160", c.memPctSum)
	}
	if !approxEq(c.diskPctSum, 140) {
		t.Fatalf("diskPctSum: got %v want 140", c.diskPctSum)
	}
}

func TestAggregate_PopulatesPercentilesAndDisk(t *testing.T) {
	c := newTestCollector()

	// Feed 100 evenly spaced values for each metric (1..100).
	for i := 1; i <= 100; i++ {
		c.update(&osStat{
			CPUPct:  float64(i),
			MemPct:  float64(i),
			DiskPct: float64(i),
		})
	}
	c.memTotalMB = 8192
	c.diskTotalMB = 102400
	c.cpuTotal = 4

	c.Aggregate()

	if c.stats.P50MemUsagePct != 50 || c.stats.P90MemUsagePct != 90 ||
		c.stats.P95MemUsagePct != 95 || c.stats.P99MemUsagePct != 99 {
		t.Fatalf("mem percentiles wrong: %+v", c.stats)
	}
	if c.stats.P50CPUUsagePct != 50 || c.stats.P90CPUUsagePct != 90 ||
		c.stats.P95CPUUsagePct != 95 || c.stats.P99CPUUsagePct != 99 {
		t.Fatalf("cpu percentiles wrong: %+v", c.stats)
	}
	if c.stats.P95DiskUsagePct != 95 {
		t.Fatalf("disk P95 wrong: got %v want 95", c.stats.P95DiskUsagePct)
	}
	if !approxEq(c.stats.AvgDiskUsagePct, 50.5) {
		t.Fatalf("AvgDiskUsagePct: got %v want 50.5", c.stats.AvgDiskUsagePct)
	}
	if c.stats.TotalDiskMB != 102400 {
		t.Fatalf("TotalDiskMB: got %v want 102400", c.stats.TotalDiskMB)
	}
	if c.stats.TotalMemMB != 8192 {
		t.Fatalf("TotalMemMB: got %v want 8192", c.stats.TotalMemMB)
	}
	if c.stats.CPUCores != 4 {
		t.Fatalf("CPUCores: got %v want 4", c.stats.CPUCores)
	}
}

func approxEq(a, b float64) bool {
	return math.Abs(a-b) < 1e-9
}
