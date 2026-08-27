// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pc

import (
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractAndValidatePreservesPCOff(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
	}{
		{name: "PC namespace absent", envs: map[string]string{"CI": "true"}},
		{name: "explicitly disabled", envs: map[string]string{EnvEnabled: "false", "CI": "true"}},
		{name: "disabled payload is stripped and ignored", envs: map[string]string{
			EnvEnabled: "false", EnvClientID: "client", "CI": "true",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ExtractAndValidate(tt.envs)
			require.NoError(t, err)
			require.False(t, cfg.Enabled)
			require.Equal(t, "true", tt.envs["CI"])
			for key := range tt.envs {
				require.NotContains(t, key, "HARNESS_PC_")
			}
		})
	}
}

func TestValidateRejectsInvalidEnabledValue(t *testing.T) {
	envs := map[string]string{EnvEnabled: "sometimes", "CI": "true"}
	_, err := ExtractAndValidate(envs)
	require.ErrorContains(t, err, "enabled value must be true or false")
	require.Equal(t, map[string]string{"CI": "true"}, envs)
}

func TestExtractAndValidateValidPrivateConnectivityContract(t *testing.T) {
	opaqueValue := "test-value"
	envs := map[string]string{
		EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: opaqueValue,
		EnvHostname: "stage-123", EnvTag: runnerTag, "HARNESS_PC_FUTURE": "value", "CI": "true",
	}

	cfg, err := ExtractAndValidate(envs)

	require.NoError(t, err)
	require.True(t, cfg.Enabled)
	require.Equal(t, opaqueValue, cfg.OIDCToken)
	require.Equal(t, map[string]string{"CI": "true"}, envs)
}

func TestValidateRejectsIncompletePrivateConnectivityIdentity(t *testing.T) {
	tests := []struct {
		name string
		envs map[string]string
	}{
		{name: "missing client ID", envs: map[string]string{
			EnvEnabled: "true", EnvOIDCToken: "token", EnvHostname: "stage-123", EnvTag: runnerTag,
		}},
		{name: "missing OIDC token", envs: map[string]string{
			EnvEnabled: "true", EnvClientID: "client", EnvHostname: "stage-123", EnvTag: runnerTag,
		}},
		{name: "missing hostname", envs: map[string]string{
			EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token", EnvHostname: " ", EnvTag: runnerTag,
		}},
		{name: "missing tag", envs: map[string]string{
			EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token", EnvHostname: "stage-123",
		}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ExtractAndValidate(tt.envs)
			require.ErrorContains(t, err, "private connectivity identity is incomplete")
		})
	}
}

func TestValidateRejectsUnsupportedPrivateConnectivityTag(t *testing.T) {
	envs := map[string]string{
		EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token",
		EnvHostname: "stage-123", EnvTag: "tag:customer-controlled",
	}
	_, err := ExtractAndValidate(envs)
	require.ErrorContains(t, err, "unsupported private connectivity tag")
}

func TestWIFClientIDAddsEphemeralPreauthorizedClaims(t *testing.T) {
	require.Equal(t, "client?ephemeral=true&preauthorized=true", wifClientID("client"))
	require.Equal(t, "client?audience=harness&ephemeral=true&preauthorized=true",
		wifClientID("client?audience=harness"))
}

func TestClearProxyEnvironment(t *testing.T) {
	for _, key := range []string{"http_proxy", "HTTPS_PROXY", "All_Proxy"} {
		t.Setenv(key, "http://previous-stage-proxy")
	}
	t.Setenv("HARNESS_HTTPS_PROXY", "preserved")
	t.Setenv("PROXY_URL", "preserved")

	ClearProxyEnvironment()

	for _, key := range []string{"http_proxy", "HTTPS_PROXY", "All_Proxy"} {
		_, present := os.LookupEnv(key)
		require.False(t, present, key)
	}
	require.Equal(t, "preserved", os.Getenv("HARNESS_HTTPS_PROXY"))
	require.Equal(t, "preserved", os.Getenv("PROXY_URL"))
}
