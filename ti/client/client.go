package client

import (
	"context"

	"github.com/harness/lite-engine/ti"
)

// Error represents a json-encoded API error.
type Error struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

func (e *Error) Error() string {
	return e.Message
}

// Client defines a TI service client.
type Client interface {
	// Write test cases to DB
	Write(ctx context.Context, step, report string, tests []*ti.TestCase) error

	// SelectTests returns list of tests which should be run intelligently
	SelectTests(step, source, target, req string) (ti.SelectTestsResp, error)

	// UploadCg uploads avro encoded callgraph to ti server
	UploadCg(step, source, target string, timeMs int64, cg []byte) error
}
