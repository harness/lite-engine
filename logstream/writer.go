// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"io"
)

// Writer needs to implement io.Writer
type Writer interface {
	io.WriteCloser
	Open() error
	Start()
	Error() error // Track if any error was recorded
}

type nopWriter struct {
	data []string
}

func (*nopWriter) Start()       {}
func (*nopWriter) Open() error  { return nil }
func (*nopWriter) Close() error { return nil }
func (*nopWriter) Error() error { return nil }
func (n *nopWriter) Write(p []byte) (int, error) {
	n.data = append(n.data, string(p))
	return len(p), nil
}

func NopWriter() Writer {
	return new(nopWriter)
}
