// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package filestore

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/harness/lite-engine/logstream"
)

func TestFileStore_OpenWriteClose(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	ctx := context.Background()

	if err := fs.Open(ctx, "k1"); err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := fs.Write(ctx, "k1", []*logstream.Line{{Message: "hi", Number: 0}}); err != nil {
		t.Fatalf("write: %v", err)
	}
	if err := fs.Close(ctx, "k1", false); err != nil {
		t.Fatalf("close: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "k1")); err != nil {
		t.Fatalf("stat: %v", err)
	}
}

// TestFileStore_ConcurrentOpenWriteClose drives independent keys in parallel
// — each goroutine owns its own key for its full lifecycle. Stresses the
// state map under concurrent insertion/lookup/mutation.
func TestFileStore_ConcurrentOpenWriteClose(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	ctx := context.Background()

	const goroutines = 32
	var wg sync.WaitGroup
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k-%d", i)
			if err := fs.Open(ctx, key); err != nil {
				t.Errorf("open[%d]: %v", i, err)
				return
			}
			for j := 0; j < 20; j++ {
				lines := []*logstream.Line{{Message: fmt.Sprintf("line-%d-%d", i, j), Number: j}}
				if err := fs.Write(ctx, key, lines); err != nil {
					t.Errorf("write[%d,%d]: %v", i, j, err)
					return
				}
			}
			if err := fs.Close(ctx, key, false); err != nil {
				t.Errorf("close[%d]: %v", i, err)
			}
		}(i)
	}
	wg.Wait()
}

// TestFileStore_ConcurrentWritesSameKey opens a single key and pounds it from
// many goroutines. os.File.Write is itself goroutine-safe, but this also
// exercises concurrent reads of the state map via getFileRef().
func TestFileStore_ConcurrentWritesSameKey(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	ctx := context.Background()

	const key = "shared"
	if err := fs.Open(ctx, key); err != nil {
		t.Fatalf("open: %v", err)
	}

	const writers = 16
	const linesPer = 50
	var wg sync.WaitGroup
	for i := 0; i < writers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; j < linesPer; j++ {
				lines := []*logstream.Line{{Message: fmt.Sprintf("g%d-%d", i, j), Number: j}}
				if err := fs.Write(ctx, key, lines); err != nil {
					t.Errorf("write[%d,%d]: %v", i, j, err)
					return
				}
			}
		}(i)
	}
	wg.Wait()

	if err := fs.Close(ctx, key, false); err != nil {
		t.Fatalf("close: %v", err)
	}
}

// TestFileStore_CloseRacesWrite races Close() against in-flight Write()s.
// Write() captures the file pointer under the mutex via getFileRef() and
// then writes outside the lock — meanwhile Close() can close the file.
// We don't assert no errors here (a write to a closed fd is expected to
// return EBADF), only that the race detector stays clean.
func TestFileStore_CloseRacesWrite(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	ctx := context.Background()

	const key = "closing"
	if err := fs.Open(ctx, key); err != nil {
		t.Fatalf("open: %v", err)
	}

	var wg sync.WaitGroup
	stop := make(chan struct{})

	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			for j := 0; ; j++ {
				select {
				case <-stop:
					return
				default:
				}
				_ = fs.Write(ctx, key, []*logstream.Line{{Message: "x", Number: j}})
			}
		}(i)
	}

	// Let writes get going, then close.
	for i := 0; i < 100; i++ {
		_ = fs.Write(ctx, key, []*logstream.Line{{Message: "warmup", Number: i}})
	}
	_ = fs.Close(ctx, key, false)
	close(stop)
	wg.Wait()
}

// TestFileStore_ConcurrentMixedKeys runs a mix of operations on overlapping
// key sets — closer to real handler load where multiple steps interleave
// open/write/close on different keys at the same time.
func TestFileStore_ConcurrentMixedKeys(t *testing.T) {
	dir := t.TempDir()
	fs := New(dir)
	ctx := context.Background()

	const keys = 8
	const workers = 16

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			key := fmt.Sprintf("k-%d", i%keys)
			// Each worker tries the full lifecycle; Open may fail if the
			// file already exists, but we don't care here — we're stressing
			// the state map, not asserting semantics.
			_ = fs.Open(ctx, key)
			for j := 0; j < 10; j++ {
				_ = fs.Write(ctx, key, []*logstream.Line{{Message: "m", Number: j}})
			}
			_ = fs.Close(ctx, key, false)
		}(i)
	}
	wg.Wait()
}
