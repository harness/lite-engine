package osstats

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/mem"
	"github.com/sirupsen/logrus"
)

// NDJSONStreamer collects OS stats once per second and appends newline-delimited JSON
// objects to a file. Each line is a single JSON object.
//
// The JSON line format matches:
// {"<timestamp>":{"totalMemory":<val>,"totalCPU":<val>,"avaMemory":<val>,"avalCPU":<val>}}
//
// Note: Memory values are in MB. CPU values:
// - totalCPU: number of cores
// - avalCPU: available CPU percent (100 - usedPercent)
type NDJSONStreamer struct {
	ctx  context.Context
	log  *logrus.Entry
	path string

	done chan struct{}
	wg   sync.WaitGroup

	mu     sync.Mutex
	file   *os.File
	writer *bufio.Writer
}

type osStatsPayload struct {
	TotalMemory float64 `json:"totalMemory"`
	TotalCPU    int     `json:"totalCPU"`
	AvaMemory   float64 `json:"avaMemory"`
	AvalCPU     float64 `json:"avalCPU"`
}

// SanitizeFilename converts an arbitrary string into a safe filename segment.
func SanitizeFilename(in string) string {
	// Keep this minimal and deterministic. Avoid path separators and characters that
	// are likely problematic on Windows.
	r := strings.NewReplacer("/", "_", "\\", "_", ":", "_")
	s := r.Replace(in)

	// Many log keys are long (and can exceed common filename limits like 255 bytes).
	// If the sanitized name is too long, truncate and append a short hash.
	const maxLen = 200 // conservative across platforms/filesystems //nolint:mnd
	if len(s) <= maxLen {
		return s
	}

	sum := sha256.Sum256([]byte(s))
	short := fmt.Sprintf("%x", sum[:8]) // 16 hex chars
	const prefixLen = 160               //nolint:mnd
	if prefixLen >= maxLen {
		return s[:maxLen]
	}
	return s[:prefixLen] + "_" + short
}

func NewNDJSONStreamer(ctx context.Context, filePath string, log *logrus.Entry) (*NDJSONStreamer, error) {
	if ctx == nil {
		return nil, errors.New("context is nil")
	}
	if filePath == "" {
		return nil, errors.New("filePath is empty")
	}
	if log == nil {
		log = logrus.NewEntry(logrus.StandardLogger())
	}

	f, err := os.Create(filePath)
	if err != nil {
		return nil, err
	}

	return &NDJSONStreamer{
		ctx:    ctx,
		log:    log,
		path:   filePath,
		done:   make(chan struct{}),
		file:   f,
		writer: bufio.NewWriterSize(f, 64*1024), //nolint:mnd
	}, nil
}

func (s *NDJSONStreamer) Path() string { return s.path }

func (s *NDJSONStreamer) Start() {
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		s.run()
	}()
}

func (s *NDJSONStreamer) Stop() {
	select {
	case <-s.done:
		// already stopped
	default:
		close(s.done)
	}
	s.wg.Wait()

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer != nil {
		_ = s.writer.Flush()
	}
	if s.file != nil {
		_ = s.file.Sync()
		_ = s.file.Close()
	}
	s.writer = nil
	s.file = nil
}

func (s *NDJSONStreamer) run() {
	// Prime CPU percent calculation (gopsutil uses time delta between calls).
	_, _ = cpu.Percent(0, false)

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-s.done:
			return
		default:
		}

		rec, err := s.sample()
		if err == nil {
			s.append(rec)
		} else {
			s.log.WithError(err).Debugln("osstats: failed to sample")
		}
	}
}

func (s *NDJSONStreamer) sample() (map[string]osStatsPayload, error) {
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

	ts := time.Now().UTC().Format(time.RFC3339Nano)
	return map[string]osStatsPayload{
		ts: {
			TotalMemory: formatMB(vm.Total),
			TotalCPU:    totalCPU,
			AvaMemory:   formatMB(vm.Available),
			AvalCPU:     avalCPU,
		},
	}, nil
}

func (s *NDJSONStreamer) append(rec map[string]osStatsPayload) {
	b, err := json.Marshal(rec)
	if err != nil {
		s.log.WithError(err).Debugln("osstats: failed to marshal record")
		return
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.writer == nil {
		return
	}
	_, _ = s.writer.Write(b)
	_, _ = s.writer.Write([]byte("\n"))
	_ = s.writer.Flush()
}
