// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package livelog

import (
	"context"
	"fmt"
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

// TestSetIntervalRace exercises the New() -> SetInterval() pattern that
// previously raced with the Start goroutine reading b.interval. The race
// detector must report no warning here. SetInterval is now backed by an
// atomic.Int64 so the spawned reader and post-construction writer are safe.
func TestSetIntervalRace(t *testing.T) {
	w := New(context.Background(), new(mockClient), "race", "race", nil, false, false, false, false)
	// Concurrently write the interval while the Start goroutine reads it.
	done := make(chan struct{})
	go func() {
		for i := 0; i < 1000; i++ {
			w.SetInterval(time.Duration(i+1) * time.Microsecond)
		}
		close(done)
	}()
	<-done
	w.Close()
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
