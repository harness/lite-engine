// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"context"
	"time"

	"github.com/sirupsen/logrus"

	"github.com/harness/lite-engine/telemetry"
)

// telemetryShutdownTimeout bounds how long server exit may wait on telemetry.
const telemetryShutdownTimeout = 5 * time.Second

// shutdownTelemetry delivers buffered telemetry and releases its resources.
// It is a no-op when telemetry was never started, and never waits longer than
// telemetryShutdownTimeout, so an unreachable backend cannot hold the process up.
func shutdownTelemetry() {
	ctx, cancel := context.WithTimeout(context.Background(), telemetryShutdownTimeout)
	defer cancel()
	if err := telemetry.Default().Shutdown(ctx); err != nil {
		logrus.WithError(err).Warnln("failed to shut down telemetry cleanly")
	}
}
