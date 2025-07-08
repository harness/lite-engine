// Copyright 2024 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package runtime

import (
	"os"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
)

// checkBuildToolMarkers checks for marker files in /tmp directory
// and sets the corresponding flags in the telemetry data
func checkBuildToolMarkers(telemetryData *types.TelemetryData, log *logrus.Logger) {
	// Check Maven marker file
	if checkMarkerFileExists("/tmp/bi-maven", log) {
		telemetryData.BuildIntelligenceMetaData.IsMavenBIUsed = true
	}
	
	// Check Gradle marker file
	if checkMarkerFileExists("/tmp/bi-gradle", log) {
		telemetryData.BuildIntelligenceMetaData.IsGradleBIUsed = true
	}
	
	// Check Bazel marker file
	if checkMarkerFileExists("/tmp/bi-bazel", log) {
		telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed = true
	}
}

// checkMarkerFileExists checks if a marker file exists and logs if found
func checkMarkerFileExists(path string, log *logrus.Logger) bool {
	if _, err := os.Stat(path); err == nil {
		log.Debugf("Build tool marker detected: %s", path)
		return true
	}
	return false
}
