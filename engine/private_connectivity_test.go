// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"testing"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/stretchr/testify/require"
)

func TestPrivateConnectivityDNSIsLimitedToConfiguredPCContainers(t *testing.T) {
	tests := []struct {
		name     string
		cfg      *spec.PipelineConfig
		step     *spec.Step
		expected []string
	}{
		{
			name: "hosted Linux PC container",
			cfg: &spec.PipelineConfig{
				PrivateConnectivity: true,
				Platform:            spec.Platform{OS: "linux"},
			},
			step: &spec.Step{}, expected: []string{"100.100.100.100"},
		},
		{
			name: "hosted Windows PC container",
			cfg: &spec.PipelineConfig{
				PrivateConnectivity: true,
				Platform:            spec.Platform{OS: "windows"},
			},
			step: &spec.Step{}, expected: []string{"100.100.100.100"},
		},
		{
			name: "PC off is unchanged",
			cfg: &spec.PipelineConfig{
				Platform: spec.Platform{OS: "linux"},
			},
			step: &spec.Step{}, expected: nil,
		},
		{
			name: "macOS native execution is unchanged",
			cfg: &spec.PipelineConfig{
				PrivateConnectivity: true,
				Platform:            spec.Platform{OS: "darwin"},
			},
			step: &spec.Step{}, expected: nil,
		},
		{
			name: "explicit step DNS is preserved",
			cfg: &spec.PipelineConfig{
				PrivateConnectivity: true,
				Platform:            spec.Platform{OS: "windows"},
			},
			step: &spec.Step{DNS: []string{"10.0.0.53"}}, expected: []string{"10.0.0.53"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			applyPrivateConnectivityDNS(tt.cfg, tt.step)
			require.Equal(t, tt.expected, tt.step.DNS)
		})
	}
}
