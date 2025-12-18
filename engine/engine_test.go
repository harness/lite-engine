package engine

import (
	"bytes"
	"os"
	"runtime"
	"strings"
	"testing"

	"github.com/harness/lite-engine/engine/spec"
	"github.com/stretchr/testify/assert"
)

func TestRun(t *testing.T) {
	testCases := []struct {
		name       string
		entrypoint []string
		command    []string
		wantOut    string
		wantErr    bool
	}{
		{
			name:       "Simple echo command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"echo 'Hello, world!'"},
			wantOut:    "\x1b[33;1mExecuting the following command(s):\n\x1b[33;1mecho 'Hello, world!'\n",
			wantErr:    false,
		},
		{
			name:       "Complex echo command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"set -e; \necho \\n\necho hello \\n world\necho hello \\\\n world\necho \"hello \\n world\"\necho \"hello \\\\n world\""},
			wantOut: "\x1b[33;1mExecuting the following command(s):\n\x1b[33;1mset -e; \n\x1b[33;1mecho \\n\n\x1b[33;1mecho hello \\n world\n" +
				"\x1b[33;1mecho hello \\\\n world\n\x1b[33;1mecho \"hello \\n world\"\n\x1b[33;1mecho \"hello \\\\n world\"\n",
			wantErr: false,
		}, {
			name:       "Multi-line command",
			entrypoint: []string{"bash", "-c"},
			command:    []string{"echo 'Line 1' \necho 'Line 2'"},
			wantOut:    "\x1b[33;1mExecuting the following command(s):\n\x1b[33;1mecho 'Line 1' \n\x1b[33;1mecho 'Line 2'\n",
			wantErr:    false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			step := &spec.Step{
				Entrypoint: tc.entrypoint,
				Command:    tc.command,
			}
			output := &bytes.Buffer{}
			printCommand(step, output)

			gotOut := output.String()
			if !strings.Contains(gotOut, tc.wantOut) {
				t.Errorf("expected output to contain:\n%q\ngot:\n%s", tc.wantOut, gotOut)
			}
		})
	}
}

func TestRunHelper(t *testing.T) {
	cfg := &spec.PipelineConfig{
		Envs: map[string]string{
			"GLOBAL_KEY": "global_value",
		},
		Volumes: []*spec.Volume{
			{
				HostPath: &spec.VolumeHostPath{Path: "/some/path"},
			},
		},
	}

	step := &spec.Step{
		Envs: map[string]string{
			"STEP_KEY": "step_value",
		},
		WorkingDir: "/work/dir",
		Volumes: []*spec.VolumeMount{
			{Name: "myMount", Path: "/mount/path"},
		},
		Files: []*spec.File{},
	}

	// Act
	err := runHelper(cfg, step)

	// Assert
	assert.NoError(t, err)
	// Env vars should be merged
	assert.Equal(t, "global_value", step.Envs["GLOBAL_KEY"])
	assert.Equal(t, "step_value", step.Envs["STEP_KEY"])
	if runtime.GOOS == "windows" {
		assert.Equal(t, "c:\\some\\path", cfg.Volumes[0].HostPath.Path)
		assert.Equal(t, "c:\\mount\\path", step.Volumes[0].Path)
	} else {
		assert.Equal(t, "/some/path", cfg.Volumes[0].HostPath.Path)
		assert.Equal(t, "/mount/path", step.Volumes[0].Path)
	}
}

func TestToWindowsDrive(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		workdir  string
		expected string
	}{
		{
			name:     "unix path without workdir",
			input:    "/some/path",
			workdir:  "",
			expected: "c:\\some\\path",
		},
		{
			name:     "unix path with D drive workdir",
			input:    "/some/path",
			workdir:  "D:\\custom",
			expected: "d:\\some\\path",
		},
		{
			name:     "path with existing drive letter",
			input:    "C:\\existing\\path",
			workdir:  "D:\\custom",
			expected: "C:\\existing\\path",
		},
		{
			name:     "docker sock path unchanged",
			input:    DockerSockWinPath,
			workdir:  "D:\\custom",
			expected: DockerSockWinPath,
		},
		{
			name:     "docker unix sock path unchanged",
			input:    DockerSockUnixPath,
			workdir:  "D:\\custom",
			expected: DockerSockUnixPath,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Save and restore HARNESS_WORKDIR
			orig := os.Getenv("HARNESS_WORKDIR")
			defer os.Setenv("HARNESS_WORKDIR", orig)

			os.Setenv("HARNESS_WORKDIR", tt.workdir)
			result := toWindowsDrive(tt.input)
			assert.Equal(t, tt.expected, result)
		})
	}
}
