// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package cache

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ParseCacheSavings looks up the bazel marker via checkBuildToolMarkers against
// the real pipeline.SharedVolPath (a const, unreachable from tests), which is
// always marker-free in this environment. So instead of trying to redirect
// that lookup, these tests pre-set IsBazelBIUsed directly: checkBuildToolMarkers
// only ever flips flags to true and never resets them to false, so a pre-set
// true survives the real (marker-free) lookup untouched.

func TestParseCacheSavings_BazelMarkerPresent_NoGradleOrMavenReports(t *testing.T) {
	workspace := t.TempDir() // empty: no gradle/maven report files anywhere

	telemetryData := &types.TelemetryData{}
	telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed = true // simulate marker already detected

	cacheState, buildTime, _, err := ParseCacheSavings(workspace, logrus.New(), 4200, telemetryData, nil)

	assert.NoError(t, err, "bazel marker must override the gradle+maven failure gate")
	assert.Equal(t, types.OPTIMIZED, cacheState)
	assert.Equal(t, 4200, buildTime)
	assert.True(t, telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed)
}

func TestParseCacheSavings_NoMarkersNoReports_ReturnsError(t *testing.T) {
	workspace := t.TempDir() // empty: no gradle/maven reports, no BI markers

	telemetryData := &types.TelemetryData{}
	cacheState, buildTime, _, err := ParseCacheSavings(workspace, logrus.New(), 4200, telemetryData, nil)

	assert.Error(t, err, "with neither reports nor a bazel marker, the original failure gate must still apply")
	assert.Equal(t, types.FULL_RUN, cacheState)
	assert.Equal(t, 0, buildTime)
	assert.False(t, telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed)
}

// TestParseCacheSavings_GoReportViaStepEnvs ensures Cloud VM step envs are enough
// to discover go-cache-report.json under /tmp/harness/<exec>/ without process env.
func TestParseCacheSavings_GoReportViaStepEnvs(t *testing.T) {
	t.Setenv("HARNESS_TMP_PATH", "")
	t.Setenv("HARNESS_EXECUTION_ID", "")
	t.Setenv("HARNESS_GO_CACHE_REPORT_PATH", "")

	execID := "ci18519-cache-parse-ut"
	reportDir := filepath.Join("/tmp", "harness", execID)
	require.NoError(t, os.MkdirAll(reportDir, 0o755))
	t.Cleanup(func() { _ = os.RemoveAll(reportDir) })

	payload, err := json.Marshal(map[string]any{
		"version": 1, "gets": 2, "hits": 2, "misses": 0, "puts": 0, "duration_ms": 500,
	})
	require.NoError(t, err)
	require.NoError(t, os.WriteFile(filepath.Join(reportDir, "go-cache-report.json"), payload, 0o600))

	telemetryData := &types.TelemetryData{}
	stepEnvs := map[string]string{
		"HARNESS_TMP_PATH":     "/tmp/",
		"HARNESS_EXECUTION_ID": execID,
	}
	cacheState, buildTime, savings, err := ParseCacheSavings(t.TempDir(), logrus.New(), 1200, telemetryData, stepEnvs)
	require.NoError(t, err)
	assert.Equal(t, types.OPTIMIZED, cacheState)
	assert.Equal(t, 500, buildTime)
	require.Len(t, savings.GoMetrics.Reports, 1)
	assert.EqualValues(t, 2, savings.GoMetrics.Reports[0].Hits)
}
