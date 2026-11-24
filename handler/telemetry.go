// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/api"
	"github.com/harness/lite-engine/telemetry"
)

// destroyFlushTimeout bounds the final telemetry flush on destroy. The VM is
// discarded right after, so a dead backend must not hold destroy up for long.
const destroyFlushTimeout = 3 * time.Second

// telemetryStarter is the part of the telemetry controller setup needs.
type telemetryStarter interface {
	Start(cfg telemetry.Config) error
}

// telemetryFlusher is the part of the telemetry controller destroy needs.
type telemetryFlusher interface {
	Flush(ctx context.Context) error
}

// startTelemetry starts telemetry from the setup request. Telemetry is
// strictly best effort: whatever goes wrong here is logged and dropped, never
// returned, so it cannot fail or delay the stage setup.
func startTelemetry(starter telemetryStarter, cfg *api.TelemetryConfig, log *logrus.Entry) {
	if cfg == nil {
		return
	}
	defer func() {
		if r := recover(); r != nil {
			log.WithField("panic", r).Warnln("api: telemetry start panicked; continuing without telemetry")
		}
	}()
	err := starter.Start(telemetry.Config{
		Enabled:      cfg.Enabled,
		Endpoint:     cfg.Endpoint,
		MetricsVMID:  cfg.MetricsVMID,
		MetricLabels: cfg.MetricLabels,
	})
	if err != nil {
		log.WithError(err).Warnln("api: failed to start telemetry; continuing without it")
	}
}

// flushTelemetry delivers buffered telemetry, waiting at most timeout. Errors
// are logged and dropped.
func flushTelemetry(ctx context.Context, flusher telemetryFlusher, timeout time.Duration, log *logrus.Entry) {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	if err := flusher.Flush(ctx); err != nil {
		log.WithError(err).Warnln("api: failed to flush telemetry")
	}
}
