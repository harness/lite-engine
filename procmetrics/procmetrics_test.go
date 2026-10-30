// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package procmetrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEnterLeaveStep(t *testing.T) {
	before := StepsRunning()
	EnterStep()
	assert.Equal(t, before+1, StepsRunning())
	LeaveStep()
	assert.Equal(t, before, StepsRunning())
}

func TestMetricsAndPprof(t *testing.T) {
	EnterStep()
	t.Cleanup(LeaveStep)

	body := Text()
	assert.Contains(t, body, "lite_engine_goroutines ")
	assert.Contains(t, body, "lite_engine_steps_running ")
	assert.Contains(t, body, "lite_engine_open_fds ")
	assert.Contains(t, body, "lite_engine_gc_pause_ns_total ")

	srv := httptest.NewServer(Handler())
	t.Cleanup(srv.Close)

	resp, err := http.Get(srv.URL + "/metrics")
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
	raw, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	assert.True(t, strings.Contains(string(raw), "lite_engine_goroutines "))

	pprofResp, err := http.Get(srv.URL + "/debug/pprof/")
	require.NoError(t, err)
	defer pprofResp.Body.Close()
	assert.Equal(t, http.StatusOK, pprofResp.StatusCode)
}
