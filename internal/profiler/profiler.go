// Package profiler provides always-on profiling for lite-engine.
//
// It runs two parallel mechanisms:
//
//  1. HTTP pprof endpoint on a local port (default :6060) for on-demand
//     profile fetching with `go tool pprof` or `curl`.
//  2. A periodic snapshotter that writes goroutine, mutex, block, heap, and
//     CPU profiles to disk on a rolling schedule. Older files are pruned
//     automatically.
//
// The two are independent: even if the HTTP server is wedged, the
// snapshotter continues writing to disk.
package profiler

import (
	"context"
	"fmt"
	"log"
	"net/http"
	_ "net/http/pprof" //nolint:gosec // pprof exposed only on localhost
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strings"
	"sync"
	"time"
)

// Config controls profiler behavior. Use Default() for sensible defaults.
type Config struct {
	// Dir is the root directory for snapshot files.
	Dir string

	// HTTPAddr is the local pprof HTTP listen address (e.g. "localhost:6060").
	// Empty disables the HTTP endpoint.
	HTTPAddr string

	// Interval between periodic snapshots.
	// Goroutine, mutex, block: every Interval.
	// Heap: every 6×Interval. CPU: 30s every 30×Interval.
	Interval time.Duration

	// Keep is the rolling retention count per profile type. 0 = keep all.
	Keep int

	// SlowHandlerThreshold triggers extra snapshots if a handler exceeds it.
	// Zero disables the trip-wire.
	SlowHandlerThreshold time.Duration

	// MutexProfileFraction passed to runtime.SetMutexProfileFraction.
	// 1 = sample every contention event (highest detail).
	MutexProfileFraction int

	// BlockProfileRate passed to runtime.SetBlockProfileRate.
	// 1 = sample every blocking event.
	BlockProfileRate int
}

// Default returns sensible production defaults.
func Default() Config {
	return Config{
		Dir:                  "/tmp/le-profiles",
		HTTPAddr:             "localhost:6060",
		Interval:             10 * time.Second, //nolint:mnd
		Keep:                 200,              //nolint:mnd
		SlowHandlerThreshold: 5 * time.Second,  //nolint:mnd
		MutexProfileFraction: 1,
		BlockProfileRate:     1,
	}
}

// Profiler is an always-on snapshotter + pprof HTTP server.
type Profiler struct {
	cfg  Config
	stop chan struct{}
	wg   sync.WaitGroup
}

// Start begins the snapshotter and pprof HTTP server. Call Stop to shut them
// down. Safe to call once per process.
func Start(cfg Config) (*Profiler, error) {
	if cfg.Dir == "" {
		return nil, fmt.Errorf("profiler: Dir must be set")
	}
	if cfg.Interval <= 0 {
		cfg.Interval = 10 * time.Second //nolint:mnd
	}
	if cfg.MutexProfileFraction > 0 {
		runtime.SetMutexProfileFraction(cfg.MutexProfileFraction)
	}
	if cfg.BlockProfileRate > 0 {
		runtime.SetBlockProfileRate(cfg.BlockProfileRate)
	}
	for _, sub := range []string{"goroutine", "mutex", "block", "heap", "cpu", "threadcreate", "slow"} {
		if err := os.MkdirAll(filepath.Join(cfg.Dir, sub), 0o755); err != nil { //nolint:mnd
			return nil, fmt.Errorf("profiler: mkdir %s: %w", sub, err)
		}
	}

	p := &Profiler{
		cfg:  cfg,
		stop: make(chan struct{}),
	}

	if cfg.HTTPAddr != "" {
		p.wg.Add(1)
		go p.runHTTPServer()
	}

	p.wg.Add(1)
	go p.runSnapshotter()

	log.Printf("profiler: started dir=%s http=%s interval=%s keep=%d slowHandler=%s",
		cfg.Dir, cfg.HTTPAddr, cfg.Interval, cfg.Keep, cfg.SlowHandlerThreshold)
	return p, nil
}

// Stop shuts down the profiler. Returns when all goroutines exit.
func (p *Profiler) Stop() {
	close(p.stop)
	p.wg.Wait()
}

func (p *Profiler) runHTTPServer() {
	defer p.wg.Done()
	srv := &http.Server{
		Addr:              p.cfg.HTTPAddr,
		ReadHeaderTimeout: 5 * time.Second, //nolint:mnd
	}
	go func() {
		<-p.stop
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second) //nolint:mnd
		defer cancel()
		_ = srv.Shutdown(ctx)
	}()
	if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		log.Printf("profiler: http server exited: %v", err)
	}
}

func (p *Profiler) runSnapshotter() {
	defer p.wg.Done()
	tick := time.NewTicker(p.cfg.Interval)
	defer tick.Stop()

	heapEvery := 6 * p.cfg.Interval //nolint:mnd
	cpuEvery := 30 * p.cfg.Interval //nolint:mnd
	cpuDuration := 30 * time.Second //nolint:mnd
	if cpuDuration > cpuEvery/2 {   //nolint:mnd
		cpuDuration = cpuEvery / 2 //nolint:mnd
	}

	var lastHeap, lastCPU time.Time
	for {
		select {
		case <-p.stop:
			p.snapshotAll("shutdown")
			return
		case t := <-tick.C:
			ts := t.UTC().Format("20060102-150405")
			p.writeGoroutine(ts)
			p.writeProfile("mutex", ts)
			p.writeProfile("block", ts)
			p.writeProfile("threadcreate", ts)
			if t.Sub(lastHeap) >= heapEvery {
				p.writeProfile("heap", ts)
				lastHeap = t
			}
			if t.Sub(lastCPU) >= cpuEvery {
				go p.writeCPU(ts, cpuDuration)
				lastCPU = t
			}
			p.prune("goroutine")
			p.prune("mutex")
			p.prune("block")
			p.prune("threadcreate")
			p.prune("heap")
			p.prune("cpu")
		}
	}
}

// snapshotAll writes a one-shot snapshot of every profile under <dir>/slow/<tag>-*.
// Used at shutdown and by SnapshotNow / SlowHandler trip-wire.
func (p *Profiler) snapshotAll(tag string) string {
	ts := time.Now().UTC().Format("20060102-150405.000")
	prefix := filepath.Join(p.cfg.Dir, "slow", fmt.Sprintf("%s-%s", tag, ts))

	// goroutine debug=2 (full stacks)
	if f, err := os.Create(prefix + ".goroutine.txt"); err == nil {
		buf := make([]byte, 1<<20) //nolint:mnd
		for {
			n := runtime.Stack(buf, true)
			if n < len(buf) {
				_, _ = f.Write(buf[:n])
				break
			}
			buf = make([]byte, 2*len(buf)) //nolint:mnd
		}
		_ = f.Close()
	}
	// other profiles
	for _, name := range []string{"mutex", "block", "heap", "threadcreate", "allocs"} {
		if f, err := os.Create(prefix + "." + name + ".pb.gz"); err == nil {
			if pp := pprof.Lookup(name); pp != nil {
				_ = pp.WriteTo(f, 0)
			}
			_ = f.Close()
		}
	}
	log.Printf("profiler: snapshot taken path=%s", prefix)
	return prefix
}

// SnapshotNow takes an immediate snapshot tagged with the given label.
// Returns the prefix path (without extension).
func (p *Profiler) SnapshotNow(tag string) string {
	return p.snapshotAll(sanitizeTag(tag))
}

// WatchHandler wraps fn and triggers a snapshot if it runs longer than
// the configured SlowHandlerThreshold. Use to wrap HTTP handlers.
func (p *Profiler) WatchHandler(name string, fn func()) {
	if p.cfg.SlowHandlerThreshold <= 0 {
		fn()
		return
	}
	done := make(chan struct{})
	t := time.AfterFunc(p.cfg.SlowHandlerThreshold, func() {
		select {
		case <-done:
		default:
			p.snapshotAll("slow-" + sanitizeTag(name))
		}
	})
	defer t.Stop()
	defer close(done)
	fn()
}

func (p *Profiler) writeGoroutine(ts string) {
	full := filepath.Join(p.cfg.Dir, "goroutine", ts+".txt")
	if f, err := os.Create(full); err == nil {
		buf := make([]byte, 1<<20) //nolint:mnd
		for {
			n := runtime.Stack(buf, true)
			if n < len(buf) {
				_, _ = f.Write(buf[:n])
				break
			}
			buf = make([]byte, 2*len(buf)) //nolint:mnd
		}
		_ = f.Close()
	}
}

func (p *Profiler) writeProfile(name, ts string) {
	pp := pprof.Lookup(name)
	if pp == nil {
		return
	}
	path := filepath.Join(p.cfg.Dir, name, ts+".pb.gz")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	_ = pp.WriteTo(f, 0)
}

func (p *Profiler) writeCPU(ts string, duration time.Duration) {
	path := filepath.Join(p.cfg.Dir, "cpu", ts+".pb.gz")
	f, err := os.Create(path)
	if err != nil {
		return
	}
	defer f.Close()
	if err := pprof.StartCPUProfile(f); err != nil {
		return
	}
	select {
	case <-time.After(duration):
	case <-p.stop:
	}
	pprof.StopCPUProfile()
}

func (p *Profiler) prune(sub string) {
	if p.cfg.Keep <= 0 {
		return
	}
	dir := filepath.Join(p.cfg.Dir, sub)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	if len(entries) <= p.cfg.Keep {
		return
	}
	type fi struct {
		name string
		mod  time.Time
	}
	files := make([]fi, 0, len(entries))
	for _, e := range entries {
		if e.IsDir() {
			continue
		}
		info, err := e.Info()
		if err != nil {
			continue
		}
		files = append(files, fi{e.Name(), info.ModTime()})
	}
	sort.Slice(files, func(i, j int) bool { return files[i].mod.Before(files[j].mod) })
	excess := len(files) - p.cfg.Keep
	for i := 0; i < excess; i++ {
		_ = os.Remove(filepath.Join(dir, files[i].name))
	}
}

func sanitizeTag(s string) string {
	r := strings.NewReplacer("/", "_", " ", "_", ":", "_", "?", "_", "&", "_")
	return r.Replace(s)
}
