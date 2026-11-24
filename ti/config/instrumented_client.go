// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package config

import (
	"context"

	v2types "github.com/harness/ti-client/chrysalis/types"
	"github.com/harness/ti-client/client"
	"github.com/harness/ti-client/types"

	"github.com/harness/lite-engine/telemetry"
)

// instrumentedClient decorates the TI service client. It embeds the interface,
// so every method is delegated untouched, and overrides only the calls whose
// health lite-engine reports: test report writes and callgraph uploads.
type instrumentedClient struct {
	client.Client
	rec telemetry.Recorder
}

// instrumentTIClient wraps next so report and callgraph calls are reported to rec.
func instrumentTIClient(next client.Client, rec telemetry.Recorder) client.Client {
	return &instrumentedClient{Client: next, rec: rec}
}

func (c *instrumentedClient) observe(op telemetry.Operation, call func() error) error {
	return telemetry.Observe(c.rec, telemetry.SubsystemTIService, op, call)
}

// Write implements client.Client.
func (c *instrumentedClient) Write(ctx context.Context, step, report string, tests []*types.TestCase) error {
	return c.observe(telemetry.OpTIReport, func() error { return c.Client.Write(ctx, step, report, tests) })
}

// UploadCg implements client.Client.
func (c *instrumentedClient) UploadCg(ctx context.Context, step, source, target string, timeMs int64, cg []byte, failedTestRerunEnabled bool) error {
	return c.observe(telemetry.OpTIUploadCallgraph, func() error {
		return c.Client.UploadCg(ctx, step, source, target, timeMs, cg, failedTestRerunEnabled)
	})
}

// UploadCgV2 implements client.Client.
//
//nolint:gocritic // hugeParam: the signature is dictated by client.Client
func (c *instrumentedClient) UploadCgV2(ctx context.Context, payload v2types.UploadCgRequest, stepID string, timeMs int64, sourceBranch, targetBranch string) error {
	return c.observe(telemetry.OpTIUploadCallgraph, func() error {
		return c.Client.UploadCgV2(ctx, payload, stepID, timeMs, sourceBranch, targetBranch)
	})
}
