package osstats

import (
	"context"
	"encoding/json"
	"io"
	"math"
	"runtime"
	"sort"
	"sync"
	"time"

	"github.com/harness/lite-engine/internal/safego"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/disk"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/sirupsen/logrus"
)

// OSStatsPayload is the JSON structure for each OS stats record.
// The JSON line format includes CPU, memory, and disk metrics.
//
// Note: Memory and disk values are in GB. CPU values:
// - totalCPU: number of cores
// - avalCPU: available CPU percent (100 - usedPercent)
// - disk: root partition (or primary mount); usedPercent is 0 if disk stats unavailable
type OSStatsPayload struct {
	TotalMemory   float64 `json:"totalMemory"`
	TotalCPU      int     `json:"totalCPU"`
	AvaMemory     float64 `json:"avaMemory"`
	AvalCPU       float64 `json:"avalCPU"`
	TotalDiskGB   float64 `json:"totalDiskGB"`
	UsedDiskGB    float64 `json:"usedDiskGB"`
	AvaDiskGB     float64 `json:"avaDiskGB"`
	UsedDiskPct   float64 `json:"usedDiskPct"`
}

// OSStatsSummaryPayload is the final NDJSON line written when streaming stops.
// It includes P90 CPU usage, total cores, and the last memory/disk metrics (same as regular lines).
type OSStatsSummaryPayload struct {
	P90CPUUsagePct float64 `json:"p90CPUUsagePct"`
	TotalCPU       int     `json:"totalCPU"`
	TotalMemory    float64 `json:"totalMemory"`
	AvaMemory      float64 `json:"avaMemory"`
	TotalDiskGB    float64 `json:"totalDiskGB"`
	UsedDiskGB     float64 `json:"usedDiskGB"`
	AvaDiskGB      float64 `json:"avaDiskGB"`
	UsedDiskPct    float64 `json:"usedDiskPct"`
}

// StartOSStatsStreaming starts a goroutine that collects OS stats once per second
// and writes JSON lines to the provided io.Writer (e.g., a livelog.Writer).
// When the returned cancel function is called, it stops the loop, computes P90 CPU
// from all samples, and writes one final NDJSON line with p90CPUUsagePct, totalCPU,
// and the last memory/disk metrics. Returns a cancel function to stop the collection.
func StartOSStatsStreaming(ctx context.Context, w io.Writer, log *logrus.Entry) (cancel func()) {
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once
	var summaryOnce sync.Once

	var cpuUsedPctSamples []float64
	var lastPayload OSStatsPayload

	wg.Add(1)
	safego.SafeGo("os_stats_streaming", func() {
		defer wg.Done()
		runOSStatsLoop(ctx, done, w, log, &cpuUsedPctSamples, &lastPayload)
	})

	return func() {
		stopOnce.Do(func() {
			select {
			case <-done:
				// already stopped
			default:
				close(done)
			}
		})
		wg.Wait()

		summaryOnce.Do(func() {
			p90 := p90NearestRank(cpuUsedPctSamples)
			writeOSStatsSummaryRecord(w, p90, &lastPayload, log)
		})
	}
}

func runOSStatsLoop(ctx context.Context, done chan struct{}, w io.Writer, log *logrus.Entry, cpuSamples *[]float64, lastPayload *OSStatsPayload) {
	// Prime CPU percent calculation (gopsutil uses time delta between calls).
	_, _ = cpu.Percent(0, false)

	for {
		select {
		case <-ctx.Done():
			return
		case <-done:
			return
		default:
		}

		rec, usedCPU, err := sampleOSStats()
		if err == nil {
			writeOSStatsRecord(w, rec, log)
			if cpuSamples != nil {
				*cpuSamples = append(*cpuSamples, usedCPU)
			}
			// Keep last payload for the final summary line (memory + disk)
			if lastPayload != nil && len(rec) == 1 {
				for _, p := range rec {
					*lastPayload = p
					break
				}
			}
		} else {
			log.WithError(err).Debugln("osstats: failed to sample")
		}
	}
}

func sampleOSStats() (map[string]OSStatsPayload, float64, error) {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		return nil, 0, err
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, 0, err
	}

	totalCPU := runtime.NumCPU()
	usedCPU := percent[0]
	avalCPU := 100.0 - usedCPU
	if avalCPU < 0 {
		avalCPU = 0
	}

	payload := OSStatsPayload{
		TotalMemory: formatGB(vm.Total),
		TotalCPU:    totalCPU,
		AvaMemory:   formatGB(vm.Available),
		AvalCPU:     avalCPU,
	}

	// Disk usage for root (or primary) partition; same gopsutil package as cpu/mem
	if du, err := disk.Usage("/"); err == nil {
		payload.TotalDiskGB = formatGB(du.Total)
		payload.UsedDiskGB = formatGB(du.Used)
		payload.AvaDiskGB = formatGB(du.Free)
		payload.UsedDiskPct = du.UsedPercent
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	return map[string]OSStatsPayload{ts: payload}, usedCPU, nil
}

func writeOSStatsRecord(w io.Writer, rec map[string]OSStatsPayload, log *logrus.Entry) {
	b, err := json.Marshal(rec)
	if err != nil {
		log.WithError(err).Debugln("osstats: failed to marshal record")
		return
	}

	// Write JSON followed by newline (NDJSON format)
	_, _ = w.Write(append(b, '\n'))
}

func writeOSStatsSummaryRecord(w io.Writer, p90CPUPct float64, last *OSStatsPayload, log *logrus.Entry) {
	if w == nil {
		return
	}
	summary := OSStatsSummaryPayload{
		P90CPUUsagePct: p90CPUPct,
		TotalCPU:       last.TotalCPU,
		TotalMemory:    last.TotalMemory,
		AvaMemory:      last.AvaMemory,
		TotalDiskGB:    last.TotalDiskGB,
		UsedDiskGB:     last.UsedDiskGB,
		AvaDiskGB:      last.AvaDiskGB,
		UsedDiskPct:    last.UsedDiskPct,
	}
	rec := map[string]OSStatsSummaryPayload{
		time.Now().UTC().Format(time.RFC3339Nano): summary,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		log.WithError(err).Debugln("osstats: failed to marshal summary record")
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

// p90NearestRank computes the 90th percentile (nearest-rank) of the given samples.
func p90NearestRank(values []float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := make([]float64, len(values))
	copy(cp, values)
	sort.Float64s(cp)
	idx := int(math.Ceil(0.90*float64(len(cp)))) - 1 //nolint:mnd
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}
