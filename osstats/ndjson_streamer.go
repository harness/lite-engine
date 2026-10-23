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

// diskIOState tracks disk I/O counters between samples to calculate throughput
// and total bytes transferred over the streaming session.
type diskIOState struct {
	firstReadBytes  uint64
	firstWriteBytes uint64
	prevReadBytes   uint64
	prevWriteBytes  uint64
	lastReadBytes   uint64
	lastWriteBytes  uint64
	initialized     bool
}

// Payload is the JSON structure for each OS stats record.
// The JSON line format includes CPU, memory, and disk I/O metrics.
//
// Note: Memory values are in GB. CPU values:
// - totalCPU: number of cores
// - avalCPU: available CPU percent (100 - usedPercent)
// - diskIO: read/write throughput in bytes per second
type Payload struct {
	TotalMemory       float64 `json:"totalMemory"`
	TotalCPU          int     `json:"totalCPU"`
	AvaMemory         float64 `json:"avaMemory"`
	AvalCPU           float64 `json:"avalCPU"`
	DiskReadBytesSec  float64 `json:"diskReadBytesSec"`
	DiskWriteBytesSec float64 `json:"diskWriteBytesSec"`
}

// SummaryPayload is the final NDJSON line written when streaming stops.
// It embeds Payload for memory/disk metrics and adds the three CPU summary
// metrics (peak, avgUtilization, p90). osStatsSummary is always true so consumers
// can reliably identify this line (e.g. grep "osStatsSummary").
type SummaryPayload struct {
	OSStatsSummary      bool    `json:"osStatsSummary"`      // true = this line is the summary (last line)
	PeakCPUUsagePct     float64 `json:"peakCPUUsagePct"`     // max (peak) CPU utilization %
	AvgCPUUsagePct      float64 `json:"avgCPUUsagePct"`      // average CPU utilization %
	P90CPUUsagePct      float64 `json:"p90CPUUsagePct"`      // P90 CPU utilization %
	TotalDiskReadBytes  uint64  `json:"totalDiskReadBytes"`  // total bytes read during execution
	TotalDiskWriteBytes uint64  `json:"totalDiskWriteBytes"` // total bytes written during execution
	Payload                     // Embedded: TotalCPU, TotalMemory, AvaMemory, AvalCPU, disk fields
}

// DiskIOTotals contains the total bytes read/written during the streaming session.
type DiskIOTotals struct {
	ReadBytes  uint64
	WriteBytes uint64
}

// StreamerSummary holds all the data collected by the streamer, returned via GetSummaryData
// after the streaming goroutine has been stopped. The caller uses this to build the
// resource utilization stats for the destroy response.
type StreamerSummary struct {
	CPUSamples  []float64
	MemSamples  []float64
	LastPayload Payload
	DiskTotals  DiskIOTotals
}

// StartOSStatsStreaming starts a goroutine that collects OS stats once per second
// and writes JSON lines to the provided io.Writer. Returns (1) a cancel function to
// stop the collection and (2) getSummaryData to read the collected CPU and memory
// samples, last payload, and disk I/O totals after cancel. The caller must write the
// summary to the stream (via WriteP90SummaryToStream) before closing the writer.
func StartOSStatsStreaming(ctx context.Context, w io.Writer, log *logrus.Entry) (cancel func(), getSummaryData func() StreamerSummary) {
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once

	var cpuUsedPctSamples []float64
	var memUsedPctSamples []float64
	var lastPayload Payload
	var ioState diskIOState

	wg.Add(1)
	safego.SafeGo("os_stats_streaming", func() {
		defer wg.Done()
		runOSStatsLoop(ctx, done, w, log, &cpuUsedPctSamples, &memUsedPctSamples, &lastPayload, &ioState)
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

	getSummaryData = func() StreamerSummary {
		var totals DiskIOTotals
		if ioState.initialized && ioState.lastReadBytes >= ioState.firstReadBytes {
			totals.ReadBytes = ioState.lastReadBytes - ioState.firstReadBytes
		}
		if ioState.initialized && ioState.lastWriteBytes >= ioState.firstWriteBytes {
			totals.WriteBytes = ioState.lastWriteBytes - ioState.firstWriteBytes
		}
		return StreamerSummary{
			CPUSamples:  cpuUsedPctSamples,
			MemSamples:  memUsedPctSamples,
			LastPayload: lastPayload,
			DiskTotals:  totals,
		}
	}

	return cancel, getSummaryData
}

func runOSStatsLoop(ctx context.Context, done chan struct{}, w io.Writer, log *logrus.Entry, cpuSamples *[]float64, memSamples *[]float64, lastPayload *Payload, ioState *diskIOState) {
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

		rec, usedCPU, err := sampleOSStats(ioState)
		if err == nil {
			writeOSStatsRecord(w, rec, log)
			if cpuSamples != nil {
				*cpuSamples = append(*cpuSamples, usedCPU)
			}
			// Keep last payload for the final summary line (memory + disk)
			if lastPayload != nil && len(rec) == 1 {
				for _, p := range rec {
					*lastPayload = p
					// Compute memory used % from total and available, then track for percentiles
					if memSamples != nil && p.TotalMemory > 0 {
						memUsedPct := ((p.TotalMemory - p.AvaMemory) / p.TotalMemory) * maxCPUPercent
						*memSamples = append(*memSamples, memUsedPct)
					}
					break
				}
			}
		} else {
			log.WithError(err).Debugln("osstats: failed to sample")
		}
	}
}

func sampleOSStats(ioState *diskIOState) (rec map[string]Payload, usedCPU float64, err error) {
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

	// Disk I/O throughput: calculate bytes/sec from delta between samples
	if ioCounters, err := disk.IOCounters(); err == nil && len(ioCounters) > 0 {
		var totalReadBytes, totalWriteBytes uint64
		for name := range ioCounters {
			totalReadBytes += ioCounters[name].ReadBytes
			totalWriteBytes += ioCounters[name].WriteBytes
		}

		if ioState.initialized {
			// Calculate delta (bytes per second since last sample)
			// Guard against counter reset (reboot, wrap)
			if totalReadBytes >= ioState.prevReadBytes {
				payload.DiskReadBytesSec = float64(totalReadBytes - ioState.prevReadBytes)
			}
			if totalWriteBytes >= ioState.prevWriteBytes {
				payload.DiskWriteBytesSec = float64(totalWriteBytes - ioState.prevWriteBytes)
			}
		} else {
			// First sample: record initial values for total calculation
			ioState.firstReadBytes = totalReadBytes
			ioState.firstWriteBytes = totalWriteBytes
		}

		// Update state for next sample and track last values for totals
		ioState.prevReadBytes = totalReadBytes
		ioState.prevWriteBytes = totalWriteBytes
		ioState.lastReadBytes = totalReadBytes
		ioState.lastWriteBytes = totalWriteBytes
		ioState.initialized = true
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

// WriteP90SummaryToStream writes the final summary line to the stream with CPU metrics
// (peak, avgUtilization, p90) and total disk I/O bytes. Call this after stopping the
// collection (cancel returned from StartOSStatsStreaming) and before closing the writer,
// so the memory_metrics file always ends with this line.
func WriteP90SummaryToStream(w io.Writer, cpuSamples []float64, lastPayload Payload, diskTotals DiskIOTotals, log *logrus.Entry) {
	if w == nil {
		return
	}
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}
	peak, avg := PeakAndAvg(cpuSamples)
	p90 := PercentileNearestRank(cpuSamples, 0.90) //nolint:mnd
	summary := SummaryPayload{
		OSStatsSummary:      true,
		PeakCPUUsagePct:     peak,
		AvgCPUUsagePct:      avg,
		P90CPUUsagePct:      p90,
		TotalDiskReadBytes:  diskTotals.ReadBytes,
		TotalDiskWriteBytes: diskTotals.WriteBytes,
		Payload:             lastPayload,
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

// PeakAndAvg returns the max (peak) and average of the given usage samples.
func PeakAndAvg(values []float64) (peak, avg float64) {
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

// PercentileNearestRank computes the given percentile (0..1) using the nearest-rank
// method. Returns 0 for an empty slice.
func PercentileNearestRank(values []float64, p float64) float64 {
	if len(values) == 0 {
		return 0
	}
	cp := make([]float64, len(values))
	copy(cp, values)
	sort.Float64s(cp)
	idx := int(math.Ceil(p*float64(len(cp)))) - 1
	if idx < 0 {
		idx = 0
	}
	if idx >= len(cp) {
		idx = len(cp) - 1
	}
	return cp[idx]
}
