// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"context"
	"io"

	drone "github.com/drone/runner-go/pipeline/runtime"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/telemetry"
)

// trackedRunFunc decorates f so rec learns when a step starts and when it
// stops, however it stops (including a panic, which is not swallowed). Because
// it wraps the function that actually runs the step, detached steps are
// tracked for their whole lifetime rather than just until they were launched.
func trackedRunFunc(f RunFunc, rec telemetry.Recorder) RunFunc {
	return func(ctx context.Context, step *spec.Step, output io.Writer, isDrone, isHosted bool) (*drone.State, error) {
		defer telemetry.TrackStep(rec)()
		return f(ctx, step, output, isDrone, isHosted)
	}
}
