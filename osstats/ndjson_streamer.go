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
// The JSON line format matches:
// {"<timestamp>":{"totalMemory":<val>,"totalCPU":<val>,"avaMemory":<val>,"avalCPU":<val>,"totalDisk":<val>,"avaDisk":<val>}}
//
// Note: Memory and Disk values are in MB. CPU values:
// - totalCPU: number of cores
// - avalCPU: available CPU percent (100 - usedPercent)
type OSStatsPayload struct {
	TotalMemory float64 `json:"totalMemory"`
	TotalCPU    int     `json:"totalCPU"`
	AvaMemory   float64 `json:"avaMemory"`
	AvalCPU     float64 `json:"avalCPU"`
	TotalDisk   float64 `json:"totalDisk"`
	AvaDisk     float64 `json:"avaDisk"`
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

	wg.Add(1)
	safego.SafeGo("os_stats_streaming", func() {
		defer wg.Done()
		runOSStatsLoop(ctx, done, w, log)
	})

	return func() {
		select {
		case <-done:
			// already stopped
		default:
			close(done)
		}
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

	du, err := disk.Usage(defaultDiskUsagePath())
	if err != nil {
		return nil, err
	}

	totalCPU := runtime.NumCPU()
	usedCPU := percent[0]
	avalCPU := 100.0 - usedCPU
	if avalCPU < 0 {
		avalCPU = 0
	}

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	return map[string]OSStatsPayload{
		ts: {
			TotalMemory: formatMB(vm.Total),
			TotalCPU:    totalCPU,
			AvaMemory:   formatMB(vm.Available),
			AvalCPU:     avalCPU,
			TotalDisk:   formatMB(du.Total),
			AvaDisk:     formatMB(du.Free),
		},
	}, nil
}

func defaultDiskUsagePath() string {
	// disk.Usage expects a mount point or path. "/" works for unix-likes; for
	// windows we default to the system drive.
	if runtime.GOOS == "windows" {
		return `C:\`
	}
	return "/"
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
