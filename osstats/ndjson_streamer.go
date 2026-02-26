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

const maxCPUPercent = 100.0

// Payload is the JSON structure for each OS stats record.
// The JSON line format includes CPU, memory, and disk metrics.
//
// Note: Memory and disk values are in GB. CPU values:
// - totalCPU: number of cores
// - avalCPU: available CPU percent (100 - usedPercent)
// - disk: root partition (or primary mount); usedPercent is 0 if disk stats unavailable
type Payload struct {
	TotalMemory float64 `json:"totalMemory"`
	TotalCPU    int     `json:"totalCPU"`
	AvaMemory   float64 `json:"avaMemory"`
	AvalCPU     float64 `json:"avalCPU"`
	TotalDiskGB float64 `json:"totalDiskGB"`
	UsedDiskGB  float64 `json:"usedDiskGB"`
	AvaDiskGB   float64 `json:"avaDiskGB"`
	UsedDiskPct float64 `json:"usedDiskPct"`
}

// SummaryPayload is the final NDJSON line written when streaming stops.
// It embeds Payload for memory/disk metrics and adds the three CPU summary
// metrics (peak, avgUtilization, p90). osStatsSummary is always true so consumers
// can reliably identify this line (e.g. grep "osStatsSummary").
type SummaryPayload struct {
	OSStatsSummary  bool    `json:"osStatsSummary"`  // true = this line is the summary (last line)
	PeakCPUUsagePct float64 `json:"peakCPUUsagePct"` // max (peak) CPU utilization %
	AvgCPUUsagePct  float64 `json:"avgCPUUsagePct"`  // average CPU utilization %
	P90CPUUsagePct  float64 `json:"p90CPUUsagePct"`  // P90 CPU utilization %
	Payload                 // Embedded: TotalCPU, TotalMemory, AvaMemory, AvalCPU, disk fields
}

// StartOSStatsStreaming starts a goroutine that collects OS stats once per second
// and writes JSON lines to the provided io.Writer. Returns (1) a cancel function to
// stop the collection and (2) getSummaryData to read the collected CPU samples and
// last payload after cancel. The caller must write the P90 summary to the stream
// (via WriteP90SummaryToStream) before closing the writer.
func StartOSStatsStreaming(ctx context.Context, w io.Writer, log *logrus.Entry) (cancel func(), getSummaryData func() (cpuSamples []float64, lastPayload Payload)) {
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once

	var cpuUsedPctSamples []float64
	var lastPayload Payload

	wg.Add(1)
	safego.SafeGo("os_stats_streaming", func() {
		defer wg.Done()
		runOSStatsLoop(ctx, done, w, log, &cpuUsedPctSamples, &lastPayload)
	})

	cancel = func() {
		stopOnce.Do(func() {
			select {
			case <-done:
				// already stopped
			default:
				close(done)
			}
		})
		wg.Wait()
	}

	getSummaryData = func() ([]float64, Payload) {
		return cpuUsedPctSamples, lastPayload
	}

	return cancel, getSummaryData
}

func runOSStatsLoop(ctx context.Context, done chan struct{}, w io.Writer, log *logrus.Entry, cpuSamples *[]float64, lastPayload *Payload) {
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

func sampleOSStats() (rec map[string]Payload, usedCPU float64, err error) {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		return nil, 0, err
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, 0, err
	}

	totalCPU := runtime.NumCPU()
	usedCPU = percent[0]
	avalCPU := maxCPUPercent - usedCPU
	if avalCPU < 0 {
		avalCPU = 0
	}

	payload := Payload{
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
	return map[string]Payload{ts: payload}, usedCPU, nil
}

func writeOSStatsRecord(w io.Writer, rec map[string]Payload, log *logrus.Entry) {
	b, err := json.Marshal(rec)
	if err != nil {
		log.WithError(err).Debugln("osstats: failed to marshal record")
		return
	}

	// Write JSON followed by newline (NDJSON format)
	_, _ = w.Write(append(b, '\n'))
}

// WriteP90SummaryToStream writes the final summary line to the stream with the three
// CPU metrics: peak (max), avgUtilization (average), and p90. Call this after
// stopping the collection (cancel returned from StartOSStatsStreaming) and before
// closing the writer, so the memory_metrics file always ends with this line.
func WriteP90SummaryToStream(w io.Writer, cpuSamples []float64, lastPayload Payload, log *logrus.Entry) {
	if w == nil {
		return
	}
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}
	peak, avg := peakAndAvg(cpuSamples)
	p90 := p90NearestRank(cpuSamples)
	summary := SummaryPayload{
		OSStatsSummary:  true,
		PeakCPUUsagePct: peak,
		AvgCPUUsagePct:  avg,
		P90CPUUsagePct:  p90,
		Payload:         lastPayload,
	}
	rec := map[string]SummaryPayload{
		time.Now().UTC().Format(time.RFC3339Nano): summary,
	}
	b, err := json.Marshal(rec)
	if err != nil {
		log.WithError(err).Debugln("osstats: failed to marshal summary record")
		return
	}
	_, _ = w.Write(append(b, '\n'))
}

// peakAndAvg returns the max (peak) and average of the given CPU usage samples.
func peakAndAvg(values []float64) (peak, avg float64) {
	if len(values) == 0 {
		return 0, 0
	}
	var sum float64
	for _, v := range values {
		if v > peak {
			peak = v
		}
		sum += v
	}
	return peak, sum / float64(len(values))
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
