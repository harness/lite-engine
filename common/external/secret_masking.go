// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package external

import (
	"bytes"
	"strings"

	"github.com/harness/lite-engine/logstream"
)

// bufferedStreamWriter is a minimal implementation of logstream.Writer that writes to a buffer.
// It's designed to support MaskString's integration with logstream.NewReplacer().
type bufferedStreamWriter struct {
	buf *bytes.Buffer
}

func newBufferedStreamWriter() *bufferedStreamWriter {
	return &bufferedStreamWriter{buf: &bytes.Buffer{}}
}

func (b *bufferedStreamWriter) Write(p []byte) (n int, err error) {
	return b.buf.Write(p)
}

func (b *bufferedStreamWriter) Open() error  { return nil }
func (b *bufferedStreamWriter) Start()       {}
func (b *bufferedStreamWriter) Close() error { return nil }
func (b *bufferedStreamWriter) Error() error { return nil }

// MaskString masks secrets in the given input string using the logstream replacer.
// This provides a simple interface for masking secrets in plain strings without
// needing to set up the full logstream infrastructure.
//
// Returns the input string with secrets replaced by asterisks (*******).
// If no secrets are provided or masking fails, returns the original input.
func MaskString(input string, secrets []string) string {
	return MaskStringWithEnvs(input, secrets, nil)
}

func MaskStringWithEnvs(input string, secrets []string, envs map[string]string) string {
	if len(secrets) == 0 {
		return input
	}

	bufWriter := newBufferedStreamWriter()
	replacer := logstream.NewReplacerWithEnvs(bufWriter, secrets, envs)
	_, err := replacer.Write([]byte(input))
	if err != nil {
		return input
	}
	result := bufWriter.buf.String()

	// Fallback: if any secret is still present after masking, replace it directly
	for _, secret := range secrets {
		if secret != "" && strings.Contains(result, secret) {
			result = strings.ReplaceAll(result, secret, "**************")
		}
	}

	return result
}
