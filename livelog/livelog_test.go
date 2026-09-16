// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drone/runner-go/client"
	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/logstream"
)

func TestLineWriterSingle(t *testing.T) {
	client := new(mockClient)
	w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
	w.SetInterval(time.Duration(0))
	w.num = 4
	_, _ = w.Write([]byte("foo\nbar\n"))

	a := w.pending
	b := []*logstream.Line{
		{Number: 4, Message: "foo\n"},
		{Number: 5, Message: "bar\n"},
	}
	if err := compare(a, b); err != nil {
		t.Fail()
		fmt.Print(a)
		t.Log(err)
	}

	w.Close()
	a = client.uploaded
	if err := compare(a, b); err != nil {
		t.Fail()
		t.Log(err)
	}
}

func TestSetLimit(t *testing.T) {
	client := new(mockClient)
	w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
	w.SetLimit(5000)
	w.mu.Lock()
	got := w.limit
	w.mu.Unlock()
	if got != 5000 {
		t.Fatalf("expected limit 5000, got %d", got)
	}
	w.Close()
}

func TestLineWriterSingleWithTrimNewLineSuffixEnabled(t *testing.T) {
	client := new(mockClient)
	w := New(context.Background(), client, "1", "1", nil, false, true, false, false)
	w.SetInterval(time.Duration(0))
	w.num = 4
	_, _ = w.Write([]byte("foo\nbar\n"))

	a := w.pending
	b := []*logstream.Line{
		{Number: 4, Message: "foo"},
		{Number: 5, Message: "bar"},
	}
	if err := compare(a, b); err != nil {
		t.Fail()
		fmt.Print(a)
		t.Log(err)
	}

	w.Close()
	a = client.uploaded
	if err := compare(a, b); err != nil {
		t.Fail()
		t.Log(err)
	}
}

func compare(a, b []*logstream.Line) error {
	if len(a) != len(b) {
		return fmt.Errorf("expected size: %d, actual: %d", len(a), len(b))
	}

	for i := 0; i < len(a); i++ {
		if a[i].Number != b[i].Number {
			return fmt.Errorf("expected number: %d, actual: %d", a[i].Number, b[i].Number)
		}
		if a[i].Message != b[i].Message {
			return fmt.Errorf("expected message: %s, actual: %s", a[i].Message, b[i].Message)
		}
	}
	return nil
}

type mockClient struct {
	client.Client
	lines     []*logstream.Line
	uploaded  []*logstream.Line
	openErr   error
	writeErr  error
	closeErr  error
	uploadErr error
}

func (m *mockClient) Upload(ctx context.Context, key string, lines []*logstream.Line) error {
	m.uploaded = lines
	return m.uploadErr
}

func (m *mockClient) Open(ctx context.Context, key string) error {
	return m.openErr
}

// Close closes the data stream.
func (m *mockClient) Close(ctx context.Context, key string, force bool) error {
	return m.closeErr
}

// Write writes logs to the data stream.
func (m *mockClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	m.lines = append(m.lines, lines...)
	return m.writeErr
}

// concurrentMockClient is a thread-safe mock used by the race-detector tests.
// The plain mockClient appends to its slices without synchronization, which
// would itself race when driven from multiple goroutines.
type concurrentMockClient struct {
	client.Client
	mu          sync.Mutex
	writeCalls  int32
	uploadCalls int32
	closeCalls  int32
	openCalls   int32
	lines       []*logstream.Line
	uploaded    []*logstream.Line
}

func (m *concurrentMockClient) Open(ctx context.Context, key string) error {
	atomic.AddInt32(&m.openCalls, 1)
	return nil
}

func (m *concurrentMockClient) Close(ctx context.Context, key string, force bool) error {
	atomic.AddInt32(&m.closeCalls, 1)
	return nil
}

func (m *concurrentMockClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	atomic.AddInt32(&m.writeCalls, 1)
	m.mu.Lock()
	m.lines = append(m.lines, lines...)
	m.mu.Unlock()
	return nil
}

func (m *concurrentMockClient) Upload(ctx context.Context, key string, lines []*logstream.Line) error {
	atomic.AddInt32(&m.uploadCalls, 1)
	m.mu.Lock()
	m.uploaded = append(m.uploaded, lines...)
	m.mu.Unlock()
	return nil
}

// TestWriter_ConcurrentWriteAndFlush stresses the writer with many concurrent
// producers while the background flusher (started by New) drains pending lines
// in parallel. Run with `go test -race -count=10` to catch ordering issues.
func TestWriter_ConcurrentWriteAndFlush(t *testing.T) {
	const (
		writers       = 16
		linesPerGo    = 200
		flushInterval = 1 * time.Millisecond
	)

	mc := &concurrentMockClient{}
	w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
	w.SetInterval(flushInterval)
	if err := w.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}

	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < linesPerGo; j++ {
				_, _ = fmt.Fprintf(w, "g%d-line%d\n", id, j)
			}
		}(i)
	}

	// Concurrent explicit flushers — Flush() can race with Start()'s flush
	// loop and with concurrent Writes mutating pending/history.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 20; j++ {
				_ = w.Flush()
				time.Sleep(time.Millisecond)
			}
		}()
	}

	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestWriter_ConcurrentCloseAndWrite makes Close race with in-flight Writes.
// Close calls stop() (closes b.close), then flush(); concurrent writers may
// still be appending to pending/history when stop sets b.closed=true. The
// guard inside Write must prevent writes-after-close without deadlocking.
func TestWriter_ConcurrentCloseAndWrite(t *testing.T) {
	mc := &concurrentMockClient{}
	w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
	w.SetInterval(time.Millisecond)
	if err := w.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_, _ = fmt.Fprintf(w, "g%d-%d\n", id, j)
			}
		}(i)
	}

	time.Sleep(20 * time.Millisecond)
	closeErr := w.Close()
	close(stop)
	wg.Wait()

	if closeErr != nil {
		t.Fatalf("close: %v", closeErr)
	}
}

// TestWriter_ConcurrentSettersAndWrite races setters (SetLimit/SetInterval)
// against in-flight Writes. These setters mutate fields that Write/flush read
// without holding b.mu, so this exposes any unprotected access.
func TestWriter_ConcurrentSettersAndWrite(t *testing.T) {
	mc := &concurrentMockClient{}
	w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
	w.SetInterval(time.Millisecond)
	if err := w.Open(); err != nil {
		t.Fatalf("open: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-stop:
				return
			default:
			}
			w.SetLimit(1024 * 1024)
			w.SetInterval(time.Millisecond)
		}
	}()

	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				_, _ = fmt.Fprintf(w, "g%d-%d\n", id, j)
			}
		}(i)
	}

	time.Sleep(50 * time.Millisecond)
	close(stop)
	wg.Wait()

	if err := w.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
}

func TestRecordOp_FailedRPCDoesNotAddLatency(t *testing.T) {
	ops := []string{"open", "write", "close", "upload"}
	for _, op := range ops {
		w := &Writer{}
		w.recordOp(op, errors.New("boom"), 50*time.Millisecond)
		s := statsFor(w, op)
		if s.Count != 1 || s.ErrorCount != 1 || s.LatencyMs != 0 {
			t.Fatalf("%s failed RPC: got count=%d errorCount=%d latencyMs=%d", op, s.Count, s.ErrorCount, s.LatencyMs)
		}
	}
}

func TestRecordOp_SuccessOnlyLatencyMix(t *testing.T) {
	w := &Writer{}
	w.recordOp("write", nil, 20*time.Millisecond)
	w.recordOp("write", errors.New("boom"), 50*time.Millisecond)
	w.recordOp("write", nil, 10*time.Millisecond)
	s := w.stats.Write
	if s.Count != 3 || s.ErrorCount != 1 || s.LatencyMs != 30 {
		t.Fatalf("got count=%d errorCount=%d latencyMs=%d want 3/1/30", s.Count, s.ErrorCount, s.LatencyMs)
	}
}

func TestLogServiceOpStatsJSONHasNoBytes(t *testing.T) {
	raw, err := json.Marshal(api.LogServiceOpStats{Count: 1, ErrorCount: 1, LatencyMs: 10})
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "bytes") {
		t.Fatalf("json must not include bytes: %s", raw)
	}
}

func TestOpenWriteCloseUpload_FailedRPCCounts(t *testing.T) {
	t.Run("open", func(t *testing.T) {
		mc := &mockClient{openErr: errors.New("boom")}
		w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
		if err := w.Open(); err == nil {
			t.Fatal("expected open error")
		}
		s := w.LogServiceStats().Open
		if s.Count != 1 || s.ErrorCount != 1 || s.LatencyMs != 0 {
			t.Fatalf("open stats count=%d errorCount=%d latencyMs=%d", s.Count, s.ErrorCount, s.LatencyMs)
		}
	})
	t.Run("write", func(t *testing.T) {
		mc := &mockClient{writeErr: errors.New("boom")}
		w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
		w.SetInterval(time.Hour)
		if err := w.Open(); err != nil {
			t.Fatalf("open: %v", err)
		}
		if _, err := w.Write([]byte("line\n")); err != nil {
			t.Fatalf("write: %v", err)
		}
		if err := w.Flush(); err == nil {
			t.Fatal("expected flush error")
		}
		s := w.LogServiceStats().Write
		if s.Count != 1 || s.ErrorCount != 1 || s.LatencyMs != 0 {
			t.Fatalf("write stats count=%d errorCount=%d latencyMs=%d", s.Count, s.ErrorCount, s.LatencyMs)
		}
		_ = w.Close()
	})
	t.Run("close", func(t *testing.T) {
		mc := &mockClient{closeErr: errors.New("boom")}
		w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
		w.SetInterval(time.Hour)
		if err := w.Open(); err != nil {
			t.Fatalf("open: %v", err)
		}
		_ = w.Close()
		s := w.LogServiceStats().Close
		if s.Count != 1 || s.ErrorCount != 1 || s.LatencyMs != 0 {
			t.Fatalf("close stats count=%d errorCount=%d latencyMs=%d", s.Count, s.ErrorCount, s.LatencyMs)
		}
	})
	t.Run("upload", func(t *testing.T) {
		mc := &mockClient{uploadErr: errors.New("boom")}
		w := New(context.Background(), mc, "k", "n", nil, false, false, false, false)
		w.SetInterval(time.Hour)
		if err := w.Open(); err != nil {
			t.Fatalf("open: %v", err)
		}
		if err := w.Close(); err == nil {
			t.Fatal("expected upload error from Close")
		}
		s := w.LogServiceStats().Upload
		if s.Count != 1 || s.ErrorCount != 1 || s.LatencyMs != 0 {
			t.Fatalf("upload stats count=%d errorCount=%d latencyMs=%d", s.Count, s.ErrorCount, s.LatencyMs)
		}
	})
}

func statsFor(w *Writer, op string) logstream.OpStats {
	switch op {
	case "open":
		return w.stats.Open
	case "write":
		return w.stats.Write
	case "close":
		return w.stats.Close
	case "upload":
		return w.stats.Upload
	default:
		return logstream.OpStats{}
	}
}
