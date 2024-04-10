package runtime

import (
	"bufio"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFetchExportedVars(t *testing.T) {
	tests := []struct {
		Name       string
		OutputFile string
		EnvMap     map[string]string
		Error      error
	}{
		{
			Name:       "env_variable_long",
			OutputFile: "testdata/long_output.txt",
			EnvMap:     nil,
			Error:      fmt.Errorf("output variable length is more than %d bytes", bufio.MaxScanTokenSize),
		},
		{
			Name:       "env_variable_short",
			OutputFile: "testdata/short_output.txt",
			EnvMap:     map[string]string{"SHORT_ENV_VAR": "value"},
			Error:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			envMap, err := fetchExportedVarsFromEnvFile(tc.OutputFile, os.Stdout, false)
			assert.Equal(t, tc.EnvMap, envMap)
			assert.Equal(t, tc.Error, err)
		})
	}
}

func TestExportedVarsWithNewVersionOfGodotEnv(t *testing.T) {
	tests := []struct {
		Name       string
		OutputFile string
		EnvMap     map[string]string
		Error      error
	}{
		{
			Name:       "env_variable_value_with_#",
			OutputFile: "testdata/value_with_hash.txt",
			EnvMap:     map[string]string{"VALUE_WITH_HASH": "value#123"},
			Error:      nil,
		},
		{
			Name:       "env_variable_with_-",
			OutputFile: "testdata/variable_with_dash.txt",
			EnvMap:     map[string]string{"VARIABLE_WITH_DASH-VARIABLE": "value"},
			Error:      nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.Name, func(t *testing.T) {
			envMap, err := fetchExportedVarsFromEnvFile(tc.OutputFile, os.Stdout, true)
			assert.Equal(t, tc.EnvMap, envMap)
			assert.Equal(t, tc.Error, err)
		})
	}
}
