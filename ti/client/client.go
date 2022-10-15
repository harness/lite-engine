// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package client

import (
	"context"
	"fmt"

	"github.com/harness/lite-engine/ti"
)

// Custom error
type Error struct {
	Code    int
	Message string
}

func (e *Error) Error() string {
	return fmt.Sprintf("%d: %s", e.Code, e.Message)
}

// Client defines a TI service client.
type Client interface {
	// Write test cases to DB
	Write(ctx context.Context, step, report string, tests []*ti.TestCase) error

	// SelectTests returns list of tests which should be run intelligently
	SelectTests(ctx context.Context, step, source, target string, in *ti.SelectTestsReq) (ti.SelectTestsResp, error)

	// UploadCg uploads avro encoded callgraph to ti server
	UploadCg(ctx context.Context, step, source, target string, timeMs int64, cg []byte) error

	// DownloadLink returns a list of links where the relevant agent artifacts can be downloaded
	DownloadLink(ctx context.Context, language, os, arch, framework string) ([]ti.DownloadLink, error)

	// GetTestTimes returns the test timing data
	GetTestTimes(ctx context.Context, in *ti.GetTestTimesReq) (ti.GetTestTimesResp, error)
}
