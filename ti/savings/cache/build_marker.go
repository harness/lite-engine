// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package cache

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
// After reading the marker file, it renames the file to prevent subsequent reads
func checkMarkerFileExists(path string, log *logrus.Logger) bool {
	if _, err := os.Stat(path); err == nil {
		log.Debugf("Build tool marker detected: %s", path)
		// Rename the file to indicate it's been processed
		processedPath := path + ".processed"
		if err := os.Rename(path, processedPath); err != nil {
			log.Warnf("Failed to mark file %s as processed: %v", path, err)
		}
		return true
	}
	return false
}
