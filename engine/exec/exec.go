package exec

import (
	"context"
	"io"
	"os/exec"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/spec"
)

func Run(ctx context.Context, step spec.Step, output io.Writer) (*runtime.State, error) {
	cmdArgs := append(step.Entrypoint[1:], step.Command...)

	cmd := exec.Command(step.Entrypoint[0], cmdArgs...)
	cmd.Dir = step.WorkingDir
	cmd.Env = toEnv(step.Envs)
	cmd.Stderr = output
	cmd.Stdout = output

	if err := cmd.Start(); err != nil {
		return nil, err
	}

	if step.Detach {
		return &runtime.State{Exited: false}, nil
	}

	err := cmd.Wait()
	if err == nil {
		return &runtime.State{ExitCode: 0, Exited: true}, nil
	}

	if exitErr, ok := err.(*exec.ExitError); ok {
		return &runtime.State{ExitCode: exitErr.ExitCode(), Exited: true}, nil
	}
	return nil, err
}

// helper function that converts a key value map of
// environment variables to a string slice in key=value
// format.
func toEnv(env map[string]string) []string {
	var envs []string
	for k, v := range env {
		if v != "" {
			envs = append(envs, k+"="+v)
		}
	}
	return envs
}
