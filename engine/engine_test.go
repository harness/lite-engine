package engine

import (
	"bytes"
	"context"
	"runtime"
	"strings"
	"sync"
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

// newRaceTestEngine builds an Engine directly without going through NewEnv()
// (which would dial the docker socket). The engine.docker reference is left
// nil because none of the methods exercised in the race tests below touch it
// — Setup is gated behind EnableDockerSetup=false, and the other tests only
// hit Engine.GetPipelineEnvs / the e.mu critical sections.
func newRaceTestEngine() *Engine {
	disabled := false
	return &Engine{
		pipelineConfig: &spec.PipelineConfig{
			EnableDockerSetup: &disabled,
			Envs:              map[string]string{},
		},
	}
}

// TestEngine_ConcurrentGetPipelineEnvs exercises the e.mu critical section
// from many readers while a writer concurrently rebinds e.pipelineConfig.
//
// GetPipelineEnvs returns the live map under the lock — this test verifies
// that pattern is safe when callers don't iterate the result. (Iteration
// would be a separate, API-level concern; callers in this repo currently
// don't iterate the returned map outside the e.mu boundary, but if that
// ever changes we'd need to copy in GetPipelineEnvs the same way we did
// for pipeline.EnvState.Get.)
func TestEngine_ConcurrentGetPipelineEnvs(t *testing.T) {
	e := newRaceTestEngine()

	const ops = 1000
	var wg sync.WaitGroup

	// Writer: rebinds e.pipelineConfig under the lock.
	wg.Add(1)
	go func() {
		defer wg.Done()
		disabled := false
		for i := 0; i < ops; i++ {
			cfg := &spec.PipelineConfig{
				EnableDockerSetup: &disabled,
				Envs:              map[string]string{"K": "V"},
			}
			e.mu.Lock()
			e.pipelineConfig = cfg
			e.mu.Unlock()
		}
	}()

	// Readers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_ = e.GetPipelineEnvs()
			}
		}()
	}
	wg.Wait()
}

// TestEngine_ConcurrentSetupAndGet drives Setup() (with docker disabled) in
// parallel with GetPipelineEnvs() readers. Each Setup call rebinds
// e.pipelineConfig under e.mu; readers must observe a coherent value.
func TestEngine_ConcurrentSetupAndGet(t *testing.T) {
	e := newRaceTestEngine()

	disabled := false
	cfgs := []*spec.PipelineConfig{
		{EnableDockerSetup: &disabled, Envs: map[string]string{"A": "1"}},
		{EnableDockerSetup: &disabled, Envs: map[string]string{"B": "2"}},
		{EnableDockerSetup: &disabled, Envs: map[string]string{"C": "3"}},
	}

	const ops = 200
	var wg sync.WaitGroup

	// Two writers using Setup — they share e.mu but compete to rebind the field.
	for w := 0; w < 2; w++ {
		wg.Add(1)
		go func(w int) {
			defer wg.Done()
			for i := 0; i < ops; i++ {
				cfg := cfgs[(w+i)%len(cfgs)]
				if err := e.Setup(context.Background(), cfg); err != nil {
					t.Errorf("Setup: %v", err)
					return
				}
			}
		}(w)
	}

	// Many readers
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < ops; j++ {
				_ = e.GetPipelineEnvs()
			}
		}()
	}
	wg.Wait()
}
