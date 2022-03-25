// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package logstream

import (
	"strings"
)

const (
	maskedStr = "**************"
)

// replacer wraps a stream writer with a replacer
type replacer struct {
	w Writer
	r *strings.Replacer
}

// NewReplacer returns a replacer that wraps io.Writer w.
func NewReplacer(w Writer, secrets []string) Writer {
	var oldnew []string
	for _, secret := range secrets {
		if secret == "" {
			continue
		}

		for _, part := range strings.Split(secret, "\n") {
			part = strings.TrimSpace(part)

			// avoid masking empty or single character
			// strings.
			if len(part) < 2 { // nolint:gomnd
				continue
			}

			oldnew = append(oldnew, part, maskedStr)
		}
	}
	if len(oldnew) == 0 {
		return w
	}
	return &replacer{
		w: w,
		r: strings.NewReplacer(oldnew...),
	}
}

// Write writes p to the base writer. The method scans for any
// sensitive data in p and masks before writing.
func (r *replacer) Write(p []byte) (n int, err error) {
	_, err = r.w.Write([]byte(r.r.Replace(string(p))))
	return len(p), err
}

// Open opens the base writer.
func (r *replacer) Open() error {
	return r.w.Open()
}

func (r *replacer) Start() {
	r.w.Start()
}

// Close closes the base writer.
func (r *replacer) Close() error {
	return r.w.Close()
}

func (r *replacer) Error() error {
	return r.w.Error()
}
