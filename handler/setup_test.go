// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package handler

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestClearProxyEnvs(t *testing.T) {
	keys := []string{"http_proxy", "https_proxy", "no_proxy", "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY"}
	for _, key := range keys {
		t.Setenv(key, "http://previous-stage-proxy")
	}
	clearProxyEnvs()
	for _, key := range keys {
		_, present := os.LookupEnv(key)
		require.False(t, present, key)
	}
}

func TestPrivateConnectivityConflictsWithEgress(t *testing.T) {
	tests := []struct {
		name     string
		enabled  bool
		proxyURL string
		envs     map[string]string
		want     bool
	}{
		{name: "PC disabled", proxyURL: "http://proxy", envs: map[string]string{harnessHTTPSProxyEnvVar: "http://proxy"}},
		{name: "no proxy", enabled: true},
		{name: "egress policy proxy", enabled: true, proxyURL: "http://proxy", want: true},
		{name: "Harness HTTPS proxy", enabled: true,
			envs: map[string]string{harnessHTTPSProxyEnvVar: "http://proxy"}, want: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			require.Equal(t, tt.want,
				privateConnectivityConflictsWithEgress(tt.enabled, tt.proxyURL, tt.envs))
		})
	}
}
