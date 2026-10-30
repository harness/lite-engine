// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package procmetrics exposes lite-engine process gauges.
// The health server serves the gauge text on /metrics. The loopback debug
// server serves the same gauges and /debug/pprof/.
package procmetrics

import (
	"fmt"
	"net/http"
	"net/http/pprof"
	"os"
	"runtime"
	"sync/atomic"
)

// Stats is one sample of process gauges.
type Stats struct {
	Goroutines     int
	StepsRunning   int64
	OpenFDs        int
	GCPauseTotalNs uint64
	NumGC          uint32
}

var stepsRunning atomic.Int64

// EnterStep marks one step as in flight.
func EnterStep() { stepsRunning.Add(1) }

// LeaveStep marks one step as finished.
func LeaveStep() { stepsRunning.Add(-1) }

// StepsRunning returns the in-flight step count.
func StepsRunning() int64 { return stepsRunning.Load() }

// Snapshot reads the current process gauges.
func Snapshot() Stats {
	var mem runtime.MemStats
	runtime.ReadMemStats(&mem)
	return Stats{
		Goroutines:     runtime.NumGoroutine(),
		StepsRunning:   stepsRunning.Load(),
		OpenFDs:        openFDs(),
		GCPauseTotalNs: mem.PauseTotalNs,
		NumGC:          mem.NumGC,
	}
}

func openFDs() int {
	dir := "/proc/self/fd"
	if runtime.GOOS == "darwin" {
		dir = "/dev/fd"
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0
	}
	return len(entries)
}

// Text renders the gauges in a Prometheus-compatible text format.
func Text() string {
	s := Snapshot()
	return fmt.Sprintf(
		"lite_engine_goroutines %d\nlite_engine_steps_running %d\nlite_engine_open_fds %d\nlite_engine_gc_pause_ns_total %d\nlite_engine_gc_num %d\n",
		s.Goroutines, s.StepsRunning, s.OpenFDs, s.GCPauseTotalNs, s.NumGC,
	)
}

// Handler serves /metrics and /debug/pprof/ on a dedicated mux.
// It does not register on http.DefaultServeMux.
func Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("/metrics", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(Text()))
	})
	mux.HandleFunc("/debug/pprof/", pprof.Index)
	mux.HandleFunc("/debug/pprof/cmdline", pprof.Cmdline)
	mux.HandleFunc("/debug/pprof/profile", pprof.Profile)
	mux.HandleFunc("/debug/pprof/symbol", pprof.Symbol)
	mux.HandleFunc("/debug/pprof/trace", pprof.Trace)
	return mux
}
