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
	envs := map[string]string{
		EnvEnabled: "false", EnvClientID: "client", EnvOIDCToken: "token",
		EnvHostname: "stage-123", EnvTag: runnerTag, "CI": "true",
	}

	cfg, err := ExtractAndValidate(envs)
	require.NoError(t, err)
	require.False(t, cfg.Enabled)
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

func TestExtractAndValidateRejectsInvalidContracts(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(map[string]string)
		message string
	}{
		{name: "invalid enabled value", mutate: func(envs map[string]string) { envs[EnvEnabled] = "sometimes" },
			message: "enabled value must be true or false"},
		{name: "missing OIDC token", mutate: func(envs map[string]string) { delete(envs, EnvOIDCToken) },
			message: "identity is incomplete"},
		{name: "unsupported tag", mutate: func(envs map[string]string) { envs[EnvTag] = "tag:customer-controlled" },
			message: "unsupported private connectivity tag"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			envs := validPrivateConnectivityEnvs()
			tt.mutate(envs)
			_, err := ExtractAndValidate(envs)
			require.ErrorContains(t, err, tt.message)
		})
	}
}

func validPrivateConnectivityEnvs() map[string]string {
	return map[string]string{
		EnvEnabled: "true", EnvClientID: "client", EnvOIDCToken: "token",
		EnvHostname: "stage-123", EnvTag: runnerTag,
	}
}

func TestClearProxyEnvironment(t *testing.T) {
	for _, key := range []string{"http_proxy", "HTTPS_PROXY", "All_Proxy"} {
		t.Setenv(key, "http://previous-stage-proxy")
	}
	t.Setenv("HARNESS_HTTPS_PROXY", "preserved")

	ClearProxyEnvironment()

	for _, key := range []string{"http_proxy", "HTTPS_PROXY", "All_Proxy"} {
		_, present := os.LookupEnv(key)
		require.False(t, present, key)
	}
	require.Equal(t, "preserved", os.Getenv("HARNESS_HTTPS_PROXY"))
}
