// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package telemetry

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	sdklog "go.opentelemetry.io/otel/sdk/log"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"

	"github.com/harness/lite-engine/version"
)

const (
	serviceName = "lite-engine"
	// logExportInterval is how often buffered log records are shipped.
	logExportInterval = 5 * time.Second
)

// ErrClosed is returned by Start once the Controller has been shut down.
var ErrClosed = errors.New("telemetry: controller is shut down")

// Option customizes a Controller. The constructors are unexported: they exist
// so tests can substitute exporters and process readings.
type Option func(*options)

type options struct {
	metricExporter func(Config) (sdkmetric.Exporter, error)
	logExporter    func(Config) (sdklog.Exporter, error)
	stats          ProcessStats
	logger         *logrus.Logger
	hostname       func() (string, error)
	version        string
}

func defaultOptions() options {
	return options{
		metricExporter: newOTLPMetricExporter,
		logExporter:    newOTLPLogExporter,
		stats:          newRuntimeProcessStats(),
		logger:         logrus.StandardLogger(),
		hostname:       osHostname,
		version:        version.GetVersion(),
	}
}

func withMetricExporter(f func(Config) (sdkmetric.Exporter, error)) Option {
	return func(o *options) { o.metricExporter = f }
}
func withLogExporter(f func(Config) (sdklog.Exporter, error)) Option {
	return func(o *options) { o.logExporter = f }
}
func withProcessStats(s ProcessStats) Option       { return func(o *options) { o.stats = s } }
func withLogger(l *logrus.Logger) Option           { return func(o *options) { o.logger = l } }
func withHostname(f func() (string, error)) Option { return func(o *options) { o.hostname = f } }
func withVersion(v string) Option                  { return func(o *options) { o.version = v } }

// Controller owns the telemetry lifecycle: it is started when the setup
// request delivers a Config, flushed when the stage is destroyed and shut down
// when the server exits.
//
// Everything on the hot path (the Recorder and the log hook) is lock-free.
// The mutex only guards lifecycle state and is never held across network I/O.
type Controller struct {
	opts     options
	recorder *delegatingRecorder

	mu      sync.Mutex
	current *session
	closed  bool
}

// NewController returns a Controller that has not started anything yet.
func NewController(opts ...Option) *Controller {
	o := defaultOptions()
	for _, apply := range opts {
		apply(&o)
	}
	return &Controller{opts: o, recorder: newDelegatingRecorder()}
}

var (
	defaultController     *Controller
	defaultControllerOnce sync.Once
)

// Default returns the process-wide Controller used by lite-engine.
func Default() *Controller {
	defaultControllerOnce.Do(func() { defaultController = NewController() })
	return defaultController
}

// Recorder returns a Recorder that is always safe to use: it discards signals
// until Start succeeds and after Shutdown. It may be captured once (for
// example by an HTTP client built before the setup request arrives).
func (c *Controller) Recorder() Recorder { return c.recorder }

// session is one running telemetry pipeline.
type session struct {
	cfg         Config
	meters      *sdkmetric.MeterProvider
	logs        *sdklog.LoggerProvider
	recorder    Recorder
	hook        *logHook
	sampler     Sampler
	stopSampler context.CancelFunc
	background  sync.WaitGroup
}

// Start brings telemetry up from cfg. It is idempotent (the first successful
// start wins, so a replayed setup request is harmless) and a disabled cfg is a
// no-op. An error means telemetry is not running; callers log it and carry on.
func (c *Controller) Start(cfg Config) (err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("telemetry: start panicked: %v", r)
		}
	}()
	if !cfg.Enabled {
		return nil
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.closed {
		return ErrClosed
	}
	if c.current != nil {
		return nil
	}
	cfg = cfg.withDefaults()
	if err := cfg.validate(); err != nil {
		return err
	}
	s, err := c.build(cfg)
	if err != nil {
		return err
	}
	c.install(s)
	// Sampled only now, after install has attached the log hook: the
	// resourceWatch sampler logs through that hook, and a Flush immediately
	// after Start (as the setup/destroy handlers, and tests mirroring them,
	// both do) must deterministically see this first reading rather than
	// racing a background goroutine for it.
	sampleOnce(s.sampler)
	s.startBackground()
	return nil
}

// build constructs a session without publishing anything. On error nothing is
// left running.
func (c *Controller) build(cfg Config) (*session, error) {
	vmID := resolveVMID(c.opts.hostname)
	res := resource.NewSchemaless(
		attribute.String("service.name", serviceName),
		attribute.String("service.version", c.opts.version),
	)

	metricExp, err := c.opts.metricExporter(cfg)
	if err != nil {
		return nil, fmt.Errorf("create metric exporter: %w", err)
	}
	logExp, err := c.opts.logExporter(cfg)
	if err != nil {
		shutdownQuietly(metricExp.Shutdown, cfg.ExportTimeout)
		return nil, fmt.Errorf("create log exporter: %w", err)
	}

	meters := sdkmetric.NewMeterProvider(
		sdkmetric.WithResource(res),
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp,
			sdkmetric.WithInterval(cfg.Interval), sdkmetric.WithTimeout(cfg.ExportTimeout))),
	)
	logs := sdklog.NewLoggerProvider(
		sdklog.WithResource(res),
		sdklog.WithProcessor(sdklog.NewBatchProcessor(logExp,
			sdklog.WithExportInterval(logExportInterval), sdklog.WithExportTimeout(cfg.ExportTimeout))),
	)
	s := &session{cfg: cfg, meters: meters, logs: logs, hook: newLogHook(logs.Logger(meterName), vmID, logrus.InfoLevel)}

	base := staticAttributes(cfg, vmID, c.opts.version)
	s.recorder, err = newMetricsRecorder(meters.Meter(meterName), base, cfg.MetricsVMID)
	var sampler Sampler
	if err == nil {
		sampler, err = registerProcessMetrics(meters.Meter(meterName), base, c.opts.stats, cfg.MetricsVMID)
	}
	if err != nil {
		_ = s.shutdown(context.Background())
		return nil, fmt.Errorf("create instruments: %w", err)
	}
	s.sampler = samplers{sampler, newResourceWatch(c.opts.stats, c.opts.logger, defaultLimits)}
	return s, nil
}

// startBackground hands periodic ticking of the session's sampler off to a
// background goroutine. The first reading is taken synchronously by Start,
// before this is called (see Start), so it is not repeated here. In VM-id
// mode it also starts the one-off export of the zero baselines. Neither the
// ticking nor the baseline export is awaited by Start: an unreachable
// endpoint must not delay the setup request.
func (s *session) startBackground() {
	ctx, cancel := context.WithCancel(context.Background())
	s.stopSampler = cancel
	s.background.Add(1)
	go func() {
		defer s.background.Done()
		tickLoop(ctx, s.sampler, s.cfg.Interval)
	}()
	if s.cfg.MetricsVMID {
		s.background.Add(1)
		go func() {
			defer s.background.Done()
			flushCtx, flushCancel := context.WithTimeout(ctx, s.cfg.ExportTimeout)
			defer flushCancel()
			_ = s.meters.ForceFlush(flushCtx) // failures surface through the SDK error handler
		}()
	}
}

// install publishes a built session: route SDK errors to the rate limited
// reporter, attach the log hook, then open the recorder to traffic.
func (c *Controller) install(s *session) {
	otel.SetErrorHandler(newErrorReporter(c.opts.logger))
	c.opts.logger.AddHook(s.hook)
	c.recorder.set(s.recorder)
	c.current = s
}

// Flush delivers whatever is buffered, waiting at most the export timeout.
// It returns an error when the backend could not be reached; callers must
// treat that as informational.
func (c *Controller) Flush(ctx context.Context) error {
	s := c.snapshot()
	if s == nil {
		return nil
	}
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ExportTimeout)
	defer cancel()
	return errors.Join(s.meters.ForceFlush(ctx), s.logs.ForceFlush(ctx))
}

// Shutdown stops accepting signals, delivers what is buffered and releases
// all resources, waiting at most the export timeout (and never past ctx). It
// is idempotent, and Start fails with ErrClosed afterwards.
func (c *Controller) Shutdown(ctx context.Context) error {
	c.mu.Lock()
	if c.closed {
		c.mu.Unlock()
		return nil
	}
	c.closed = true
	s := c.current
	c.current = nil
	c.mu.Unlock()

	if s == nil {
		return nil
	}
	c.recorder.set(nil)
	return s.shutdown(ctx)
}

func (c *Controller) snapshot() *session {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.current
}

// shutdown stops background work and shuts the providers down, which flushes
// them. It is bounded by both ctx and the export timeout.
func (s *session) shutdown(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, s.cfg.ExportTimeout)
	defer cancel()
	if s.stopSampler != nil {
		s.stopSampler()
	}
	waitOrDone(ctx, &s.background)
	return errors.Join(s.meters.Shutdown(ctx), s.logs.Shutdown(ctx))
}

// waitOrDone waits for wg unless ctx ends first.
func waitOrDone(ctx context.Context, wg *sync.WaitGroup) {
	done := make(chan struct{})
	go func() { wg.Wait(); close(done) }()
	select {
	case <-done:
	case <-ctx.Done():
	}
}

func shutdownQuietly(shutdown func(context.Context) error, timeout time.Duration) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	_ = shutdown(ctx)
}
