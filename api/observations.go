// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package api

import (
	"context"
	"sync"
	"time"
)

type observationsKey struct{}

// StepObservations collects TI upload tallies for one step. It is process-local
// and is not part of the start-step request JSON.
type StepObservations struct {
	mu sync.Mutex
	ti TIServiceStats
}

// TIServiceStats is the per-step tally of TI client uploads.
type TIServiceStats struct {
	ReportUpload    LogServiceOpStats `json:"report_upload"`
	CallgraphUpload LogServiceOpStats `json:"callgraph_upload"`
}

// AuxiliaryStatus is set when the step command succeeded but a log or TI upload failed.
type AuxiliaryStatus struct {
	UploadFailed bool     `json:"upload_failed"`
	Reasons      []string `json:"reasons,omitempty"`
}

// ContextWithObservations returns a child context that carries obs.
func ContextWithObservations(ctx context.Context, obs *StepObservations) context.Context {
	if ctx == nil || obs == nil {
		return ctx
	}
	return context.WithValue(ctx, observationsKey{}, obs)
}

// ObservationsFromContext returns the step observations stored on ctx.
func ObservationsFromContext(ctx context.Context) *StepObservations {
	if ctx == nil {
		return nil
	}
	obs, _ := ctx.Value(observationsKey{}).(*StepObservations)
	return obs
}

// RecordReportUpload counts one TI test-report write.
func (s *StepObservations) RecordReportUpload(err error, latency time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordOp(&s.ti.ReportUpload, err, latency)
}

// RecordCallgraphUpload counts one TI callgraph upload.
func (s *StepObservations) RecordCallgraphUpload(err error, latency time.Duration) {
	if s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	recordOp(&s.ti.CallgraphUpload, err, latency)
}

// TIStats returns a copy of the tallies, or nil when no TI upload ran.
func (s *StepObservations) TIStats() *TIServiceStats {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ti.ReportUpload.Count == 0 && s.ti.CallgraphUpload.Count == 0 {
		return nil
	}
	cp := s.ti
	return &cp
}

func recordOp(op *LogServiceOpStats, err error, latency time.Duration) {
	op.Count++
	if err != nil {
		op.ErrorCount++
		return
	}
	if latency > 0 {
		op.LatencyMs += latency.Milliseconds()
	}
}

// AuxiliaryFrom returns a status when a successful step still had an upload error.
func AuxiliaryFrom(stepSucceeded bool, logStats *LogServiceStats, ti *TIServiceStats) *AuxiliaryStatus {
	if !stepSucceeded {
		return nil
	}
	var reasons []string
	if logStats != nil && logStats.Upload.ErrorCount > 0 {
		reasons = append(reasons, "log_service_upload")
	}
	if logStats != nil && logStats.Close.ErrorCount > 0 {
		reasons = append(reasons, "log_service_close")
	}
	if ti != nil && ti.ReportUpload.ErrorCount > 0 {
		reasons = append(reasons, "ti_report_upload")
	}
	if ti != nil && ti.CallgraphUpload.ErrorCount > 0 {
		reasons = append(reasons, "ti_callgraph_upload")
	}
	if len(reasons) == 0 {
		return nil
	}
	return &AuxiliaryStatus{UploadFailed: true, Reasons: reasons}
}
