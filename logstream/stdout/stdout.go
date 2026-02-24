// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package stdout

import (
	"context"
	"fmt"

	"github.com/harness/lite-engine/logstream"
)

func New() *Logger {
	fmt.Printf("initializing a new stdout logger\n")
	return &Logger{}
}

// Logger provides a logging implementation which simply writes to stdout.
type Logger struct {
}

func (f *Logger) Upload(_ context.Context, key string, lines []*logstream.Line) error {
	return nil
}

func (f *Logger) Open(_ context.Context, key string) error {
	return nil
}

func (f *Logger) Close(_ context.Context, key string, snapshot bool) error {
	return nil
}

// Write writes logs to stdout
func (f *Logger) Write(_ context.Context, key string, lines []*logstream.Line) error {
	for _, line := range lines {
		fmt.Printf("level=%s time=%s log=%s \n", line.Level, line.Timestamp, line.Message)
	}
	return nil
}
