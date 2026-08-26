// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pc

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractAndStripPreservesPCOff(t *testing.T) {
	tests := []struct {
		name      string
		envs      map[string]string
		wantError bool
	}{
		{name: "PC namespace absent", envs: map[string]string{"CI": "true"}},
		{name: "explicitly disabled", envs: map[string]string{EnvEnabled: "false", "CI": "true"}},
		{name: "disabled with payload fails closed", envs: map[string]string{
			EnvEnabled: "false", EnvClientID: "client", "CI": "true",
		}, wantError: true},
		{name: "invalid enable value fails closed", envs: map[string]string{
			EnvEnabled: "sometimes", "CI": "true",
		}, wantError: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ExtractAndStrip(tt.envs)
			err := Validate(&cfg)
			if tt.wantError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}
			require.Equal(t, "true", tt.envs["CI"])
			for key := range tt.envs {
				require.NotContains(t, key, "HARNESS_PC_")
			}
		})
	}
}

func TestExtractAndStripValidPrivateConnectivityContract(t *testing.T) {
	opaqueValue := "test-value"
	envs := map[string]string{
		EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: opaqueValue,
		EnvHostname: "stage-123", EnvTag: DefaultTag, "CI": "true",
	}

	cfg := ExtractAndStrip(envs)

	require.NoError(t, Validate(&cfg))
	require.True(t, cfg.Enabled)
	require.Equal(t, opaqueValue, cfg.OIDCToken)
	require.Equal(t, map[string]string{"CI": "true"}, envs)
}

func TestValidateRejectsUnexpectedPrivateConnectivityField(t *testing.T) {
	envs := map[string]string{EnvEnabled: "true", "HARNESS_PC_FUTURE": "value"}
	cfg := ExtractAndStrip(envs)
	require.ErrorContains(t, Validate(&cfg), "unsupported HARNESS_PC_* field")
}

func TestValidateRejectsIncompletePrivateConnectivityIdentity(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
	}{
		{
			name: "missing client ID",
			envs: map[string]string{
				EnvEnabled: "true", EnvOIDCToken: "token", EnvHostname: "stage-123", EnvTag: DefaultTag,
			},
		},
		{
			name: "missing OIDC token",
			envs: map[string]string{
				EnvEnabled: "true", EnvClientID: "client", EnvHostname: "stage-123", EnvTag: DefaultTag,
			},
		},
		{
			name: "missing hostname",
			envs: map[string]string{
				EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token", EnvTag: DefaultTag,
			},
		},
		{
			name: "missing tag",
			envs: map[string]string{
				EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token", EnvHostname: "stage-123",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := ExtractAndStrip(tt.envs)
			require.ErrorContains(t, Validate(&cfg), "private connectivity identity is incomplete")
		})
	}
}

func TestValidateRejectsUnsupportedPrivateConnectivityTag(t *testing.T) {
	envs := map[string]string{
		EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token",
		EnvHostname: "stage-123", EnvTag: "tag:customer-controlled",
	}
	cfg := ExtractAndStrip(envs)
	require.ErrorContains(t, Validate(&cfg), "unsupported private connectivity tag")
}

func TestWIFClientIDAddsEphemeralPreauthorizedClaims(t *testing.T) {
	require.Equal(t, "client?ephemeral=true&preauthorized=true", wifClientID("client"))
	require.Equal(t, "client?audience=harness&ephemeral=true&preauthorized=true",
		wifClientID("client?audience=harness"))
}

func TestProxyEnvironmentKeyMatching(t *testing.T) {
	for _, key := range []string{"HTTP_PROXY", "https_proxy", " No_Proxy ", "all_proxy"} {
		require.True(t, isProxyEnvironmentKey(key), key)
	}
	for _, key := range []string{"PATH", "HARNESS_HTTPS_PROXY", "PROXY_URL"} {
		require.False(t, isProxyEnvironmentKey(key), key)
	}
}
