package runtime

import (
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
			Error:      fmt.Errorf("output variable length is more than 65536 bytes"),
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
			envMap, err := fetchExportedVarsFromEnvFile(tc.OutputFile, os.Stdout)
			assert.Equal(t, tc.EnvMap, envMap)
			assert.Equal(t, tc.Error, err)
		})
	}
}
