// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

// Package telemetry pushes lite-engine health signals (metrics and logs) to an
// OTLP endpoint.
//
// The package is fail-open by construction: nothing in it may fail, block or
// slow down the work lite-engine is actually doing. Recording is non-blocking,
// panics raised by a Recorder are contained, and every configuration or export
// problem degrades to "no telemetry".
package telemetry

import (
	"sync/atomic"
	"time"
)

// Subsystem identifies the remote service a call was made to.
type Subsystem string

const (
	// SubsystemLogService is the Harness log service.
	SubsystemLogService Subsystem = "logsvc"
	// SubsystemTIService is the Harness test intelligence service.
	SubsystemTIService Subsystem = "ti"
)

// Operation identifies the kind of call made to a Subsystem.
type Operation string

const (
	// OpLogOpen opens a log stream.
	OpLogOpen Operation = "open"
	// OpLogWrite writes lines to a log stream.
	OpLogWrite Operation = "write"
	// OpLogClose closes a log stream.
	OpLogClose Operation = "close"
	// OpLogUpload uploads a complete log history.
	OpLogUpload Operation = "upload"
	// OpTIReport writes a test report to the TI service.
	OpTIReport Operation = "report"
	// OpTIUploadCallgraph uploads a callgraph to the TI service.
	OpTIUploadCallgraph Operation = "upload_callgraph"
)

// Recorder receives the signals lite-engine emits about its own behavior.
// Implementations must be safe for concurrent use and must never block.
type Recorder interface {
	// ObserveCall records one call to a remote service: how long it took and
	// whether it failed.
	ObserveCall(sub Subsystem, op Operation, d time.Duration, err error)
	// StepStarted records that a step began executing.
	StepStarted()
	// StepFinished records that a step stopped executing.
	StepFinished()
}

// Nop returns a Recorder that discards everything.
func Nop() Recorder { return nopRecorder{} }

type nopRecorder struct{}

func (nopRecorder) ObserveCall(Subsystem, Operation, time.Duration, error) {}
func (nopRecorder) StepStarted()                                           {}
func (nopRecorder) StepFinished()                                          {}

// Observe runs call, records its duration and outcome on rec, and returns the
// call's error untouched. Whatever rec does (including panicking) can never
// alter the result seen by the caller.
func Observe(rec Recorder, sub Subsystem, op Operation, call func() error) error {
	start := time.Now()
	err := call()
	elapsed := time.Since(start)
	contain(func() {
		if rec != nil {
			rec.ObserveCall(sub, op, elapsed, err)
		}
	})
	return err
}

// TrackStep records that a step started and returns the function that records
// that it finished, for use as `defer telemetry.TrackStep(rec)()`. A misbehaving
// rec can never affect the step.
func TrackStep(rec Recorder) (done func()) {
	if rec == nil {
		return func() {}
	}
	contain(rec.StepStarted)
	return func() { contain(rec.StepFinished) }
}

// contain runs fn and swallows any panic it raises: telemetry must not take
// down the process it observes.
func contain(fn func()) {
	defer func() { _ = recover() }()
	fn()
}

// delegatingRecorder is a Recorder whose target can be swapped at any time.
// It lets call sites capture a Recorder once (e.g. when building an HTTP
// client) while telemetry is started later, when the setup request arrives.
// The hot path is a single atomic load; there are no locks.
type delegatingRecorder struct {
	target atomic.Pointer[Recorder]
}

func newDelegatingRecorder() *delegatingRecorder { return &delegatingRecorder{} }

// set installs rec as the target; nil reverts to discarding.
func (d *delegatingRecorder) set(rec Recorder) {
	if rec == nil {
		d.target.Store(nil)
		return
	}
	d.target.Store(&rec)
}

func (d *delegatingRecorder) current() Recorder {
	if p := d.target.Load(); p != nil {
		return *p
	}
	return nopRecorder{}
}

func (d *delegatingRecorder) ObserveCall(sub Subsystem, op Operation, dur time.Duration, err error) {
	d.current().ObserveCall(sub, op, dur, err)
}

func (d *delegatingRecorder) StepStarted()  { d.current().StepStarted() }
func (d *delegatingRecorder) StepFinished() { d.current().StepFinished() }
