package osstats

import (
	"context"
	"encoding/json"
	"runtime"
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

type StatsCollector struct {
	ctx        context.Context
	st         time.Time
	log        *logrus.Entry
	interval   time.Duration
	doneCh     chan struct{}
	stats      *spec.OSStats
	memPctSum  float64
	cpuPctSum  float64
	cpuTotal   int
	memTotalMB float64
	logProcess bool
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
		ctx:      ctx,
		log:      logger.FromContext(ctx),
		interval: interval,
		doneCh:   make(chan struct{}),
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

func (s *StatsCollector) Stop() {
	close(s.doneCh)
}

func (s *StatsCollector) Stats() *spec.OSStats {
	return s.stats
}

// downsample cpu and memory to n points using LTTB
func (s *StatsCollector) Aggregate() {
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

	s.cpuTotal = runtime.NumCPU()
	s.memTotalMB = formatMB(vm.Total)

	// log memory
	s.log.Infof("total_gb: %f, used_gb: %f, free_gb: %f, used_pct: %f, swap_total_gb: %f, swap_used_gb: %f, swap_free_gb: %f",
		formatGB(vm.Total), formatGB(vm.Used), formatGB(vm.Available), vm.UsedPercent, formatGB(swap.Total),
		formatGB(swap.Used), formatGB(swap.Free))

	// log cpu
	s.log.Infof("cpu total: %d, cpu used percent: %f", s.cpuTotal, percent[0])

	return &osStat{CPUPct: percent[0], MemPct: vm.UsedPercent, MemTotalMB: formatMB(vm.Total),
		MemAvailableMB: formatMB(vm.Available), MemUsedMB: formatMB(vm.Used), SwapMemPct: swap.UsedPercent, CPUTotal: s.cpuTotal}, nil
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
	logrus.Infoln("Process info: ", string(output))
	return nil
}

func (s *StatsCollector) update(stat *osStat) {
	if stat.MemPct > s.stats.MaxMemUsagePct {
		s.stats.MaxMemUsagePct = stat.MemPct
	}
	if stat.CPUPct > s.stats.MaxCPUUsagePct {
		s.stats.MaxCPUUsagePct = stat.CPUPct
	}
	s.memPctSum += stat.MemPct
	s.cpuPctSum += stat.CPUPct
	s.stats.MemGraph.Points = append(s.stats.MemGraph.Points, spec.Point{X: time.Since(s.st).Seconds(), Y: stat.MemPct})
	s.stats.CPUGraph.Points = append(s.stats.CPUGraph.Points, spec.Point{X: time.Since(s.st).Seconds(), Y: stat.CPUPct})
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
