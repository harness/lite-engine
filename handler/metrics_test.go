// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestProcessMetricsOnTheHealthServer(t *testing.T) {
	router := Handler(nil, nil, nil)

	metrics := httptest.NewRecorder()
	router.ServeHTTP(metrics, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	if metrics.Code != http.StatusOK {
		t.Fatalf("GET /metrics = %d, want 200", metrics.Code)
	}
	body := metrics.Body.String()
	for _, name := range []string{
		"lite_engine_goroutines",
		"lite_engine_steps_running",
		"lite_engine_open_fds",
		"lite_engine_gc_pause_ns_total",
		"lite_engine_gc_num",
	} {
		if !strings.Contains(body, name+" ") {
			t.Fatalf("/metrics body missing %s:\n%s", name, body)
		}
	}
	if strings.Contains(body, "pprof") {
		t.Fatalf("/metrics should not mention pprof:\n%s", body)
	}

	health := httptest.NewRecorder()
	router.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/healthz", nil))
	if health.Code != http.StatusOK {
		t.Fatalf("GET /healthz = %d, want 200", health.Code)
	}
	if strings.Contains(health.Body.String(), "lite_engine_goroutines") {
		t.Fatalf("/healthz included process gauges: %s", health.Body.String())
	}

	pprof := httptest.NewRecorder()
	router.ServeHTTP(pprof, httptest.NewRequest(http.MethodGet, "/debug/pprof/", nil))
	if pprof.Code != http.StatusNotFound {
		t.Fatalf("GET /debug/pprof/ = %d, want 404", pprof.Code)
	}
}
