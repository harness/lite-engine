// Copyright 2025 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package cache

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/harness/ti-client/types"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/assert"
)

func TestCheckBuildToolMarkers(t *testing.T) {
	tests := []struct {
		name        string
		markerFiles []string
		assertFn    func(t *testing.T, data *types.TelemetryData)
	}{
		{
			name:        "maven marker present",
			markerFiles: []string{"bi-maven"},
			assertFn: func(t *testing.T, data *types.TelemetryData) {
				assert.True(t, data.BuildIntelligenceMetaData.IsMavenBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsGradleBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsBazelBIUsed)
			},
		},
		{
			name:        "gradle marker present",
			markerFiles: []string{"bi-gradle"},
			assertFn: func(t *testing.T, data *types.TelemetryData) {
				assert.False(t, data.BuildIntelligenceMetaData.IsMavenBIUsed)
				assert.True(t, data.BuildIntelligenceMetaData.IsGradleBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsBazelBIUsed)
			},
		},
		{
			name:        "bazel marker present",
			markerFiles: []string{"bi-bazel"},
			assertFn: func(t *testing.T, data *types.TelemetryData) {
				assert.False(t, data.BuildIntelligenceMetaData.IsMavenBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsGradleBIUsed)
				assert.True(t, data.BuildIntelligenceMetaData.IsBazelBIUsed)
			},
		},
		{
			name:        "all markers present",
			markerFiles: []string{"bi-maven", "bi-gradle", "bi-bazel"},
			assertFn: func(t *testing.T, data *types.TelemetryData) {
				assert.True(t, data.BuildIntelligenceMetaData.IsMavenBIUsed)
				assert.True(t, data.BuildIntelligenceMetaData.IsGradleBIUsed)
				assert.True(t, data.BuildIntelligenceMetaData.IsBazelBIUsed)
			},
		},
		{
			name:        "no markers present",
			markerFiles: nil,
			assertFn: func(t *testing.T, data *types.TelemetryData) {
				assert.False(t, data.BuildIntelligenceMetaData.IsMavenBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsGradleBIUsed)
				assert.False(t, data.BuildIntelligenceMetaData.IsBazelBIUsed)
			},
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			baseDir := t.TempDir()

			for _, marker := range tt.markerFiles {
				f, err := os.Create(filepath.Join(baseDir, marker))
				assert.NoError(t, err)
				f.Close()
			}

			telemetryData := &types.TelemetryData{}
			checkBuildToolMarkers(baseDir, telemetryData, logrus.New())

			tt.assertFn(t, telemetryData)
		})
	}
}

func TestCheckBuildToolMarkers_RenamesProcessedFile(t *testing.T) {
	baseDir := t.TempDir()

	markerPath := filepath.Join(baseDir, "bi-bazel")
	f, err := os.Create(markerPath)
	assert.NoError(t, err)
	f.Close()

	telemetryData := &types.TelemetryData{}
	checkBuildToolMarkers(baseDir, telemetryData, logrus.New())

	assert.True(t, telemetryData.BuildIntelligenceMetaData.IsBazelBIUsed)

	_, err = os.Stat(markerPath)
	assert.True(t, os.IsNotExist(err), "original marker file should have been renamed away")

	_, err = os.Stat(markerPath + ".processed")
	assert.NoError(t, err, "marker file should have been renamed to .processed")
}

func TestCheckMarkerFileExists(t *testing.T) {
	dir := t.TempDir()
	log := logrus.New()

	t.Run("file does not exist", func(t *testing.T) {
		assert.False(t, checkMarkerFileExists(filepath.Join(dir, "missing"), log))
	})

	t.Run("file exists and gets renamed", func(t *testing.T) {
		path := filepath.Join(dir, "present")
		f, err := os.Create(path)
		assert.NoError(t, err)
		f.Close()

		assert.True(t, checkMarkerFileExists(path, log))
		_, err = os.Stat(path + ".processed")
		assert.NoError(t, err)
	})
}
