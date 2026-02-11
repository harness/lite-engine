package osstats

import (
	"context"
	"encoding/json"
	"io"
	"runtime"
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

// StartOSStatsStreaming starts a goroutine that collects OS stats once per second
// and writes JSON lines to the provided io.Writer (e.g., a livelog.Writer).
// Returns a cancel function to stop the collection.
func StartOSStatsStreaming(ctx context.Context, w io.Writer, log *logrus.Entry) (cancel func()) {
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}

	done := make(chan struct{})
	var wg sync.WaitGroup
	var stopOnce sync.Once

	wg.Add(1)
	safego.SafeGo("os_stats_streaming", func() {
		defer wg.Done()
		runOSStatsLoop(ctx, done, w, log)
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
	}
}

func runOSStatsLoop(ctx context.Context, done chan struct{}, w io.Writer, log *logrus.Entry) {
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

		rec, err := sampleOSStats()
		if err == nil {
			writeOSStatsRecord(w, rec, log)
		} else {
			log.WithError(err).Debugln("osstats: failed to sample")
		}
	}
}

func sampleOSStats() (map[string]OSStatsPayload, error) {
	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		return nil, err
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
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
	return map[string]OSStatsPayload{ts: payload}, nil
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
