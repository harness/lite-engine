// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"
)

// errorReportInterval bounds how often an unreachable endpoint is reported.
const errorReportInterval = time.Minute

// errorReporter is the OTel SDK error handler. Exporters report every failed
// attempt; with an unreachable endpoint that would be one log line per
// interval per signal, so reports are rate limited to one per interval. It is
// lock-free: the winner of a burst is decided by a compare-and-swap.
type errorReporter struct {
	interval time.Duration
	now      func() time.Time
	report   func(error)
	lastNano atomic.Int64 // unix nanos of the last report; 0 means never
}

// Handle implements otel.ErrorHandler.
func (r *errorReporter) Handle(err error) {
	if err == nil {
		return
	}
	now := r.now().UnixNano()
	last := r.lastNano.Load()
	if last != 0 && now-last < int64(r.interval) {
		return
	}
	if !r.lastNano.CompareAndSwap(last, now) {
		return
	}
	contain(func() { r.report(err) })
}

func newErrorReporter(logger *logrus.Logger) *errorReporter {
	return &errorReporter{
		interval: errorReportInterval,
		now:      time.Now,
		report: func(err error) {
			diagnostics(logger).WithError(err).Warn("telemetry: OTLP export failed; continuing without it")
		},
	}
}
