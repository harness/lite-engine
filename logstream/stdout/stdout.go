// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package stdout

import (
	"context"
	"fmt"

	"github.com/harness/lite-engine/logstream"
)

func New() *StdoutLogger {
	return &StdoutLogger{}
}

// StdoutLogger provides a logging abstraction which writes logs to stdout.
type StdoutLogger struct {
}

func (f *StdoutLogger) Upload(_ context.Context, key string, lines []*logstream.Line) error {
	return nil
}

// Open opens the data stream.
func (f *StdoutLogger) Open(_ context.Context, key string) error {
	return nil
}

// Close closes the data stream.
func (f *StdoutLogger) Close(_ context.Context, key string) error {
	return nil
}

// Write writes logs to stdout
func (f *StdoutLogger) Write(_ context.Context, key string, lines []*logstream.Line) error {
	for _, line := range lines {
		fmt.Printf("level=%s time=%s log=%s \n", line.Level, line.Timestamp, line.Message)
	}
	return nil
}
