// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/drone/runner-go/client"
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

func TestIncreaseLogLimitFF(t *testing.T) {
	t.Run("default limit when env var is not set", func(t *testing.T) {
		t.Setenv("HARNESS_CI_INCREASE_LOG_LIMIT", "")
		client := new(mockClient)
		w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
		w.mu.Lock()
		got := w.limit
		w.mu.Unlock()
		if got != defaultLimit {
			t.Fatalf("expected default limit %d, got %d", defaultLimit, got)
		}
		w.Close()
	})

	t.Run("increased limit when env var is true", func(t *testing.T) {
		t.Setenv("HARNESS_CI_INCREASE_LOG_LIMIT", "true")
		client := new(mockClient)
		w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
		w.mu.Lock()
		got := w.limit
		w.mu.Unlock()
		if got != increasedLimit {
			t.Fatalf("expected increased limit %d, got %d", increasedLimit, got)
		}
		w.Close()
	})

	t.Run("default limit when env var is false", func(t *testing.T) {
		t.Setenv("HARNESS_CI_INCREASE_LOG_LIMIT", "false")
		client := new(mockClient)
		w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
		w.mu.Lock()
		got := w.limit
		w.mu.Unlock()
		if got != defaultLimit {
			t.Fatalf("expected default limit %d, got %d", defaultLimit, got)
		}
		w.Close()
	})
}

// TestCloseFlushesPartialLine verifies that a partial line (no trailing newline)
// sitting in the prev buffer is not lost when Close() is called.
func TestCloseFlushesPartialLine(t *testing.T) {
	client := new(mockClient)
	w := New(context.Background(), client, "1", "1", nil, false, false, false, false)
	w.SetInterval(time.Duration(0))

	// Write a complete line followed by a partial line with no newline.
	_, _ = w.Write([]byte("complete line\n"))
	_, _ = w.Write([]byte("partial line"))

	// prev should hold the partial content
	w.mu.Lock()
	if len(w.prev) == 0 {
		t.Fatal("expected prev to hold partial line content")
	}
	w.mu.Unlock()

	w.Close()

	// Both lines should appear in the uploaded history
	if len(client.uploaded) != 2 {
		t.Fatalf("expected 2 uploaded lines, got %d", len(client.uploaded))
	}
	if client.uploaded[0].Message != "complete line\n" {
		t.Fatalf("expected first line 'complete line\\n', got %q", client.uploaded[0].Message)
	}
	if client.uploaded[1].Message != "partial line\n" {
		t.Fatalf("expected second line 'partial line\\n', got %q", client.uploaded[1].Message)
	}
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
	lines    []*logstream.Line
	uploaded []*logstream.Line
}

func (m *mockClient) Upload(ctx context.Context, key string, lines []*logstream.Line) error {
	m.uploaded = lines
	return nil
}

func (m *mockClient) Open(ctx context.Context, key string) error {
	return nil
}

// Close closes the data stream.
func (m *mockClient) Close(ctx context.Context, key string, force bool) error {
	return nil
}

// Write writes logs to the data stream.
func (m *mockClient) Write(ctx context.Context, key string, lines []*logstream.Line) error {
	m.lines = append(m.lines, lines...)
	return nil
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
