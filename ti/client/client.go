package client

import (
	"context"

	"github.com/harness/lite-engine/ti"
)

// Error represents a json-encoded API error.
type Error struct {
	Message string `json:"error_msg"`
}

func (e *Error) Error() string {
	return e.Message
}

// Client defines a TI service client.
type Client interface {
	// Write test cases to DB
	Write(ctx context.Context, step, report string, tests []*ti.TestCase) error

	// SelectTests returns list of tests which should be run intelligently
	SelectTests(ctx context.Context, step, source, target string, in *ti.SelectTestsReq) (ti.SelectTestsResp, error)

	// UploadCg uploads avro encoded callgraph to ti server
	UploadCg(ctx context.Context, step, source, target string, timeMs int64, cg []byte) error
}
