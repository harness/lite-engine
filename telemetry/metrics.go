// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"runtime"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

const (
	meterName = "github.com/harness/lite-engine/telemetry"

	metricCalls         = "lite_engine.calls"
	metricCallDuration  = "lite_engine.call.duration"
	metricStepsStarted  = "lite_engine.steps.started"
	metricStepsFinished = "lite_engine.steps.finished"
)

// Outcome is the result dimension of lite_engine.calls.
type Outcome string

const (
	// OutcomeSuccess is a call that returned no error.
	OutcomeSuccess Outcome = "success"
	// OutcomeError is a call that returned an error.
	OutcomeError Outcome = "error"
)

// callDurationBuckets are in seconds and span a fast log write to a slow,
// retried callgraph upload.
var callDurationBuckets = []float64{0.01, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30, 60}

// instrumentedCall is a (subsystem, operation) pair lite-engine is known to make.
type instrumentedCall struct {
	sub Subsystem
	op  Operation
}

// knownCalls enumerates every series lite_engine.calls can carry. It drives
// zero-baseline seeding.
var knownCalls = []instrumentedCall{
	{SubsystemLogService, OpLogOpen},
	{SubsystemLogService, OpLogWrite},
	{SubsystemLogService, OpLogClose},
	{SubsystemLogService, OpLogUpload},
	{SubsystemTIService, OpTIReport},
	{SubsystemTIService, OpTIUploadCallgraph},
}

// temporalitySelector chooses how the SDK reports sums and histograms.
//
// Fleet mode (cumulative=false) uses delta: with no per-VM label, thousands of
// VMs write the same series, so only per-interval changes can be combined
// (the backend aggregates them statefully). VM-id mode uses cumulative: each
// VM owns its series, which is what Prometheus rate() expects.
func temporalitySelector(cumulative bool) sdkmetric.TemporalitySelector {
	temporality := metricdata.DeltaTemporality
	if cumulative {
		temporality = metricdata.CumulativeTemporality
	}
	return func(sdkmetric.InstrumentKind) metricdata.Temporality { return temporality }
}

// staticAttributes are attached to every metric: operator supplied fleet
// labels, build/platform facts and, only when asked for, the VM identity.
func staticAttributes(cfg Config, vmID, version string) []attribute.KeyValue {
	attrs := make([]attribute.KeyValue, 0, len(cfg.MetricLabels)+4) //nolint:mnd // labels + version, os, arch, vm_id
	for name, value := range cfg.MetricLabels {
		attrs = append(attrs, attribute.String(name, value))
	}
	attrs = append(attrs,
		attribute.String(attrVersion, version),
		attribute.String(attrOS, runtime.GOOS),
		attribute.String(attrArch, runtime.GOARCH),
	)
	if cfg.MetricsVMID {
		attrs = append(attrs, attribute.String(attrVMID, vmID))
	}
	return attrs
}

// metricsRecorder is the Recorder that turns lite-engine signals into OTel
// instruments. All instruments are safe for concurrent use and never block,
// so the recorder holds no locks of its own.
type metricsRecorder struct {
	base          []attribute.KeyValue
	calls         metric.Int64Counter
	duration      metric.Float64Histogram
	stepsStarted  metric.Int64Counter
	stepsFinished metric.Int64Counter
}

var _ Recorder = (*metricsRecorder)(nil)

// newMetricsRecorder creates the instruments on meter. With seedBaselines,
// every known counter series is created at zero so that the first real event
// is a visible increase rather than the series' birth.
func newMetricsRecorder(meter metric.Meter, base []attribute.KeyValue, seedBaselines bool) (*metricsRecorder, error) {
	calls, err := meter.Int64Counter(metricCalls,
		metric.WithDescription("Calls lite-engine made to remote services."))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram(metricCallDuration,
		metric.WithDescription("Duration of calls lite-engine made to remote services."),
		metric.WithUnit("s"),
		metric.WithExplicitBucketBoundaries(callDurationBuckets...))
	if err != nil {
		return nil, err
	}
	started, err := meter.Int64Counter(metricStepsStarted,
		metric.WithDescription("Steps that began executing."))
	if err != nil {
		return nil, err
	}
	finished, err := meter.Int64Counter(metricStepsFinished,
		metric.WithDescription("Steps that stopped executing."))
	if err != nil {
		return nil, err
	}

	r := &metricsRecorder{base: base, calls: calls, duration: duration, stepsStarted: started, stepsFinished: finished}
	if seedBaselines {
		r.seedBaselines()
	}
	return r, nil
}

func (r *metricsRecorder) seedBaselines() {
	ctx := context.Background()
	for _, c := range knownCalls {
		for _, outcome := range []Outcome{OutcomeSuccess, OutcomeError} {
			r.calls.Add(ctx, 0, r.attrs(
				attribute.String(attrSubsystem, string(c.sub)),
				attribute.String(attrOp, string(c.op)),
				attribute.String(attrOutcome, string(outcome))))
		}
	}
	r.stepsStarted.Add(ctx, 0, r.attrs())
	r.stepsFinished.Add(ctx, 0, r.attrs())
}

// attrs returns the static attributes plus extra as a measurement option.
func (r *metricsRecorder) attrs(extra ...attribute.KeyValue) metric.MeasurementOption {
	all := make([]attribute.KeyValue, 0, len(r.base)+len(extra))
	all = append(all, r.base...)
	all = append(all, extra...)
	return metric.WithAttributes(all...)
}

func outcomeOf(err error) Outcome {
	if err != nil {
		return OutcomeError
	}
	return OutcomeSuccess
}

// ObserveCall implements Recorder.
func (r *metricsRecorder) ObserveCall(sub Subsystem, op Operation, d time.Duration, err error) {
	ctx := context.Background()
	subAttr := attribute.String(attrSubsystem, string(sub))
	opAttr := attribute.String(attrOp, string(op))
	r.calls.Add(ctx, 1, r.attrs(subAttr, opAttr, attribute.String(attrOutcome, string(outcomeOf(err)))))
	r.duration.Record(ctx, d.Seconds(), r.attrs(subAttr, opAttr))
}

// StepStarted implements Recorder.
func (r *metricsRecorder) StepStarted() { r.stepsStarted.Add(context.Background(), 1, r.attrs()) }

// StepFinished implements Recorder.
func (r *metricsRecorder) StepFinished() { r.stepsFinished.Add(context.Background(), 1, r.attrs()) }
