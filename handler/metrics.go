// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"net/http"

	"github.com/harness/lite-engine/procmetrics"
)

// HandleProcessMetrics writes the process gauges. Profiling stays on the
// loopback server; this handler does not serve pprof.
func HandleProcessMetrics() http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
		_, _ = w.Write([]byte(procmetrics.Text()))
	}
}
