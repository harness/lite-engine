// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"context"

	"github.com/harness/lite-engine/telemetry"
)

// instrumentedClient decorates a Client, reporting the duration and outcome of
// every call to a telemetry.Recorder. It changes nothing else: arguments,
// context and errors pass through untouched, and a misbehaving Recorder cannot
// affect the result.
type instrumentedClient struct {
	next Client
	rec  telemetry.Recorder
}

var _ Client = (*instrumentedClient)(nil)

// NewInstrumentedClient wraps next so its calls are reported to rec. A nil rec
// returns next unchanged.
func NewInstrumentedClient(next Client, rec telemetry.Recorder) Client {
	if rec == nil {
		return next
	}
	return &instrumentedClient{next: next, rec: rec}
}

func (c *instrumentedClient) observe(op telemetry.Operation, call func() error) error {
	return telemetry.Observe(c.rec, telemetry.SubsystemLogService, op, call)
}

// Upload implements Client.
func (c *instrumentedClient) Upload(ctx context.Context, key string, lines []*Line) error {
	return c.observe(telemetry.OpLogUpload, func() error { return c.next.Upload(ctx, key, lines) })
}

// Open implements Client.
func (c *instrumentedClient) Open(ctx context.Context, key string) error {
	return c.observe(telemetry.OpLogOpen, func() error { return c.next.Open(ctx, key) })
}

// Close implements Client.
func (c *instrumentedClient) Close(ctx context.Context, key string, snapshot bool) error {
	return c.observe(telemetry.OpLogClose, func() error { return c.next.Close(ctx, key, snapshot) })
}

// Write implements Client.
func (c *instrumentedClient) Write(ctx context.Context, key string, lines []*Line) error {
	return c.observe(telemetry.OpLogWrite, func() error { return c.next.Write(ctx, key, lines) })
}
