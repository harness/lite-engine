// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package cache

import (
	"testing"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
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

	cacheState, buildTime, _, err := ParseCacheSavings(workspace, logrus.New(), 4200, telemetryData)

	assert.NoError(t, err, "bazel marker must override the gradle+maven failure gate")
	assert.Equal(t, types.OPTIMIZED, cacheState)
	assert.Equal(t, 4200, buildTime)
	assert.True(t, telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed)
}

func TestParseCacheSavings_NoMarkersNoReports_ReturnsError(t *testing.T) {
	workspace := t.TempDir() // empty: no gradle/maven reports, no BI markers

	telemetryData := &types.TelemetryData{}
	cacheState, buildTime, _, err := ParseCacheSavings(workspace, logrus.New(), 4200, telemetryData)

	assert.Error(t, err, "with neither reports nor a bazel marker, the original failure gate must still apply")
	assert.Equal(t, types.FULL_RUN, cacheState)
	assert.Equal(t, 0, buildTime)
	assert.False(t, telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed)
}
