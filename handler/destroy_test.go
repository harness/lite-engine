// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"errors"
	"testing"

	"github.com/harness/lite-engine/osstats"
	"github.com/harness/lite-engine/pipeline"
)

// mockWriter implements logstream.Writer for testing.
type mockWriter struct {
	written   []byte
	closed    bool
	closeErr  error
	openErr   error
	errReturn error
}

func (m *mockWriter) Write(p []byte) (int, error) {
	m.written = append(m.written, p...)
	return len(p), nil
}

func (m *mockWriter) Close() error {
	m.closed = true
	return m.closeErr
}

func (m *mockWriter) Open() error  { return m.openErr }
func (m *mockWriter) Start()       {}
func (m *mockWriter) Error() error { return m.errReturn }

// ---------------------------------------------------------------------------
// closeLELogStream tests
// ---------------------------------------------------------------------------

func TestCloseLELogStream_NilWriter(t *testing.T) {
	state := pipeline.NewTestState()
	err := closeLELogStream(context.Background(), state)
	if err != nil {
		t.Fatalf("expected nil error for nil writer, got: %v", err)
	}
}

func TestCloseLELogStream_Success(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{}
	state.SetLELogWriter(w, "test-key")

	err := closeLELogStream(context.Background(), state)
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !w.closed {
		t.Fatal("expected writer to be closed")
	}
	if state.GetLELogWriter() != nil {
		t.Fatal("expected writer to be nil after close")
	}
}

func TestCloseLELogStream_CloseError(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{closeErr: errors.New("upload failed")}
	state.SetLELogWriter(w, "test-key")

	err := closeLELogStream(context.Background(), state)
	if err == nil {
		t.Fatal("expected error, got nil")
	}
	if !w.closed {
		t.Fatal("expected writer Close() to be called")
	}
}

// ---------------------------------------------------------------------------
// closeOSStatsStream tests
// ---------------------------------------------------------------------------

func TestCloseOSStatsStream_NilEntry(t *testing.T) {
	state := pipeline.NewTestState()
	summary, err := closeOSStatsStream(state, "nonexistent-key")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if summary != nil {
		t.Fatal("expected nil summary for missing entry")
	}
}

func TestCloseOSStatsStream_FullFlow(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{}
	cancelled := false

	entry := &pipeline.OSStatsEntry{
		Writer: w,
		Cancel: func() { cancelled = true },
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples: []float64{10, 20, 30, 40, 50},
				MemSamples: []float64{5, 10, 15, 20, 25},
				LastPayload: osstats.Payload{
					TotalMemory: 4.0,
					TotalCPU:    2,
					AvaMemory:   2.0,
					AvalCPU:     50,
				},
				DiskTotals: osstats.DiskIOTotals{
					ReadBytes:  1024 * 1024 * 100,
					WriteBytes: 1024 * 1024 * 200,
				},
			}
		},
	}
	state.SetOSStatsEntry("test-key", entry)

	summary, err := closeOSStatsStream(state, "test-key")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !cancelled {
		t.Fatal("expected Cancel to be called")
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
	if len(summary.CPUSamples) != 5 {
		t.Fatalf("expected 5 CPU samples, got %d", len(summary.CPUSamples))
	}
	if len(summary.MemSamples) != 5 {
		t.Fatalf("expected 5 mem samples, got %d", len(summary.MemSamples))
	}
	if summary.LastPayload.TotalCPU != 2 {
		t.Fatalf("expected TotalCPU=2, got %d", summary.LastPayload.TotalCPU)
	}
	if summary.DiskTotals.ReadBytes != 1024*1024*100 {
		t.Fatalf("expected ReadBytes=%d, got %d", 1024*1024*100, summary.DiskTotals.ReadBytes)
	}
	if !w.closed {
		t.Fatal("expected writer to be closed")
	}
	if len(w.written) == 0 {
		t.Fatal("expected summary to be written to writer")
	}
	if state.GetOSStatsEntry("test-key") != nil {
		t.Fatal("expected entry to be removed from state")
	}
}

func TestCloseOSStatsStream_WriterCloseError(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{closeErr: errors.New("close failed")}

	entry := &pipeline.OSStatsEntry{
		Writer: w,
		Cancel: func() {},
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples:  []float64{50},
				MemSamples:  []float64{25},
				LastPayload: osstats.Payload{TotalCPU: 1, TotalMemory: 2.0},
			}
		},
	}
	state.SetOSStatsEntry("test-key", entry)

	summary, err := closeOSStatsStream(state, "test-key")
	if err == nil {
		t.Fatal("expected error from writer Close()")
	}
	if summary == nil {
		t.Fatal("expected summary even when close fails")
	}
}

func TestCloseOSStatsStream_NilCancel(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{}

	entry := &pipeline.OSStatsEntry{
		Writer: w,
		Cancel: nil,
		GetSummaryData: func() osstats.StreamerSummary {
			return osstats.StreamerSummary{
				CPUSamples: []float64{10},
				MemSamples: []float64{5},
			}
		},
	}
	state.SetOSStatsEntry("test-key", entry)

	summary, err := closeOSStatsStream(state, "test-key")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if summary == nil {
		t.Fatal("expected non-nil summary")
	}
}

func TestCloseOSStatsStream_NilWriterAndGetSummaryData(t *testing.T) {
	state := pipeline.NewTestState()
	cancelled := false

	entry := &pipeline.OSStatsEntry{
		Writer:         nil,
		Cancel:         func() { cancelled = true },
		GetSummaryData: nil,
	}
	state.SetOSStatsEntry("test-key", entry)

	summary, err := closeOSStatsStream(state, "test-key")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if !cancelled {
		t.Fatal("expected Cancel to be called")
	}
	if summary != nil {
		t.Fatal("expected nil summary when GetSummaryData is nil")
	}
}

func TestCloseOSStatsStream_NilGetSummaryDataWithWriter(t *testing.T) {
	state := pipeline.NewTestState()
	w := &mockWriter{}

	entry := &pipeline.OSStatsEntry{
		Writer:         w,
		Cancel:         func() {},
		GetSummaryData: nil,
	}
	state.SetOSStatsEntry("test-key", entry)

	summary, err := closeOSStatsStream(state, "test-key")
	if err != nil {
		t.Fatalf("expected nil error, got: %v", err)
	}
	if summary != nil {
		t.Fatal("expected nil summary when GetSummaryData is nil")
	}
	if !w.closed {
		t.Fatal("expected writer to be closed even without summary data")
	}
}
