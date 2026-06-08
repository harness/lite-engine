package osstats

import (
	"context"
	"encoding/json"
	"log"
	"runtime"
	"sync"
	"time"

	lttb "github.com/dgryski/go-lttb"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/internal/safego"
	"github.com/harness/lite-engine/logger"
	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/shirou/gopsutil/v3/process"
	"github.com/sirupsen/logrus"
)

var (
	downsampleCount = 10
)

// StatsCollector samples host CPU/mem on a timer in a background goroutine.
//
// Concurrency model:
//   - mu guards every mutable field below (stats, accumulators, totals).
//   - Stop() closes doneCh and then blocks on stoppedCh, so no collector
//     goroutine is running by the time Stop() returns. The intended call
//     order is Start → ... → Stop → Aggregate → Stats; Aggregate and Stats
//     still take mu so they remain safe if a future caller invokes them
//     while collection is live.
type StatsCollector struct {
	ctx        context.Context
	st         time.Time
	log        *logrus.Entry
	interval   time.Duration
	doneCh     chan struct{} // closed by Stop() to signal the collector loop
	stoppedCh  chan struct{} // closed by the collector goroutine on exit
	logProcess bool

	mu         sync.Mutex
	stats      *spec.OSStats
	memPctSum  float64
	cpuPctSum  float64
	cpuTotal   int
	memTotalMB float64
}

type osStat struct {
	CPUPct         float64
	MemPct         float64
	MemTotalMB     float64
	MemAvailableMB float64
	MemUsedMB      float64
	CPUTotal       int // total number of cores
	SwapMemPct     float64
}

func New(ctx context.Context, interval time.Duration, logProcess bool) *StatsCollector {
	return &StatsCollector{
		ctx:       ctx,
		log:       logger.FromContext(ctx),
		interval:  interval,
		doneCh:    make(chan struct{}),
		stoppedCh: make(chan struct{}),
		stats: &spec.OSStats{
			MemGraph: &spec.Graph{
				Xmetric: "seconds",
				Ymetric: "mem_mb",
			},
			CPUGraph: &spec.Graph{
				Xmetric: "seconds",
				Ymetric: "cpu_milli",
			},
		},
		logProcess: logProcess,
	}
}

func (s *StatsCollector) Start() {
	s.st = time.Now()
	safego.SafeGo("stats_collector", s.collectStats)
}

// Stop signals the collector goroutine to exit and waits for it to do so.
// After Stop returns, no further mutation of stats/accumulators can come
// from the background goroutine.
//
// Stop is safe to call exactly once. Calling it twice panics on the second
// close — same contract as the original implementation.
func (s *StatsCollector) Stop() {
	close(s.doneCh)
	<-s.stoppedCh
}

// Stats returns a snapshot copy of the underlying spec.OSStats. The copy is
// independent of further collector activity, so the caller can read or
// JSON-encode it without holding any lock. We return by pointer to keep
// the existing API shape (handler/destroy.go assigns directly to a
// *spec.OSStats field on the response).
func (s *StatsCollector) Stats() *spec.OSStats {
	s.mu.Lock()
	defer s.mu.Unlock()

	out := *s.stats // shallow copy of scalar fields
	if s.stats.MemGraph != nil {
		mg := *s.stats.MemGraph
		mg.Points = append([]spec.Point(nil), s.stats.MemGraph.Points...)
		out.MemGraph = &mg
	}
	if s.stats.CPUGraph != nil {
		cg := *s.stats.CPUGraph
		cg.Points = append([]spec.Point(nil), s.stats.CPUGraph.Points...)
		out.CPUGraph = &cg
	}
	return &out
}

// Aggregate finalizes the collected samples into average + downsampled
// graphs. Intended to be called after Stop() so the collector goroutine
// is no longer running. Takes s.mu so concurrent callers (and any reader
// holding a reference returned by Stats()) cannot observe a torn state.
func (s *StatsCollector) Aggregate() {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.stats.MemGraph.Points) > 0 {
		s.stats.AvgMemUsagePct = s.memPctSum / float64(len(s.stats.MemGraph.Points))
	}
	if len(s.stats.CPUGraph.Points) > 0 {
		s.stats.AvgCPUUsagePct = s.cpuPctSum / float64(len(s.stats.CPUGraph.Points))
	}
	s.stats.TotalMemMB = s.memTotalMB
	s.stats.CPUCores = s.cpuTotal
	s.stats.MemGraph.Points = downsample(s.stats.MemGraph.Points, downsampleCount)
	s.stats.CPUGraph.Points = downsample(s.stats.CPUGraph.Points, downsampleCount)
}

func (s *StatsCollector) collectStats() {
	defer close(s.stoppedCh)

	stat, err := s.get()
	if err == nil {
		s.update(stat)
	}

	// Start collecting stats periodically
	timer := time.NewTimer(s.interval)
	defer timer.Stop()

	for {
		timer.Reset(s.interval)
		select {
		case <-s.ctx.Done():
			s.log.Error("context canceled")
			return
		case <-s.doneCh:
			return
		case <-timer.C:
			// collect stats here
			stat, err := s.get()
			if err != nil {
				s.log.WithError(err).Errorln("could not get stat")
				continue
			}
			s.update(stat)
		}
	}
}

func formatGB(val uint64) float64 {
	return float64(val) / (1024 * 1024 * 1024) //nolint:mnd
}

func formatMB(val uint64) float64 {
	return float64(val) / (1024 * 1024) //nolint:mnd
}

func (s *StatsCollector) get() (*osStat, error) {
	if s.logProcess {
		if err := DumpProcessInfo(); err != nil {
			s.log.Errorln("Unable to log process info", err)
		}
	}

	percent, err := cpu.Percent(time.Second, false)
	if err != nil || len(percent) == 0 {
		return nil, err
	}

	vm, err := mem.VirtualMemory()
	if err != nil {
		return nil, err
	}

	swap, err := mem.SwapMemory()
	if err != nil {
		return nil, err
	}

	cpuTotal := runtime.NumCPU()

	// log memory
	s.log.Infof("total_gb: %f, used_gb: %f, free_gb: %f, used_pct: %f, swap_total_gb: %f, swap_used_gb: %f, swap_free_gb: %f",
		formatGB(vm.Total), formatGB(vm.Used), formatGB(vm.Available), vm.UsedPercent, formatGB(swap.Total),
		formatGB(swap.Used), formatGB(swap.Free))

	// log cpu
	s.log.Infof("cpu total: %d, cpu used percent: %f", cpuTotal, percent[0])

	return &osStat{CPUPct: percent[0], MemPct: vm.UsedPercent, MemTotalMB: formatMB(vm.Total),
		MemAvailableMB: formatMB(vm.Available), MemUsedMB: formatMB(vm.Used), SwapMemPct: swap.UsedPercent, CPUTotal: cpuTotal}, nil
}

func DumpProcessInfo() error {
	// Retrieve list of processes
	processes, err := process.Processes()
	if err != nil {
		return err
	}

	var processDetails []map[string]interface{}

	// Iterate over processes and collect details
	for _, p := range processes {
		pid := p.Pid
		name, _ := p.Name()
		cpuPercent, _ := p.CPUPercent()
		memInfo, _ := p.MemoryInfo()
		cmdline, _ := p.Cmdline()
		parent, _ := p.Parent()
		status, _ := p.Status()
		user, _ := p.Username()
		tgid, _ := p.Tgid()
		threadNum, _ := p.NumThreads()

		// Add process details to the slice
		processDetails = append(processDetails, map[string]interface{}{
			"pid":         pid,
			"parent":      parent,
			"name":        name,
			"cpu_percent": cpuPercent,
			"memory":      memInfo,
			"cmdline":     cmdline,
			"status":      status,
			"user":        user,
			"tgid":        tgid,
			"thread_num":  threadNum,
		})
	}
	// Convert process details to JSON
	output, err := json.Marshal(processDetails)
	if err != nil {
		return err
	}
	// Use stdlib log instead of logrus: this function is invoked from
	// livelog.flush()'s idle-dump path. Logrus would dispatch to the
	// StreamHook and recurse into Writer.Write, which previously caused a
	// reentrant-mutex deadlock when called under b.mu.
	log.Println("Process info:", string(output))
	return nil
}

func (s *StatsCollector) update(stat *osStat) {
	elapsed := time.Since(s.st).Seconds()

	s.mu.Lock()
	defer s.mu.Unlock()

	if stat.MemPct > s.stats.MaxMemUsagePct {
		s.stats.MaxMemUsagePct = stat.MemPct
	}
	if stat.CPUPct > s.stats.MaxCPUUsagePct {
		s.stats.MaxCPUUsagePct = stat.CPUPct
	}
	s.memPctSum += stat.MemPct
	s.cpuPctSum += stat.CPUPct
	s.cpuTotal = stat.CPUTotal
	s.memTotalMB = stat.MemTotalMB
	s.stats.MemGraph.Points = append(s.stats.MemGraph.Points, spec.Point{X: elapsed, Y: stat.MemPct})
	s.stats.CPUGraph.Points = append(s.stats.CPUGraph.Points, spec.Point{X: elapsed, Y: stat.CPUPct})
}

func downsample(points []spec.Point, n int) []spec.Point {
	lttbPoints := make([]lttb.Point[float64], len(points))
	for idx := range points {
		lttbPoints[idx].X = points[idx].X
		lttbPoints[idx].Y = points[idx].Y
	}
	lttbPoints = lttb.LTTB(lttbPoints, n)
	downsampledPoints := make([]spec.Point, len(lttbPoints))
	for idx, v := range lttbPoints {
		downsampledPoints[idx] = spec.Point{X: v.X, Y: v.Y}
	}
	return downsampledPoints
}
