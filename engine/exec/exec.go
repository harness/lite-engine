// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package exec

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"strings"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/spec"
)

func Run(ctx context.Context, step *spec.Step, output io.Writer, isDrone bool) (*runtime.State, error) {
	if len(step.Entrypoint) == 0 {
		return nil, errors.New("step entrypoint cannot be empty")
	}

	// used for printing out the command
	command := make([]string, len(step.Command))
	copy(command, step.Command)

	// we shouldn't change this functionality for drone.
	if !isDrone {
		// Remove 'set -xe' and 'set -e;' from the command if it's there.
		for i, arg := range step.Command {
			step.Command[i] = strings.ReplaceAll(arg, "set -xe;", "set -e;")
		}
		// Remove 'set -xe' and '' for printing out commands
		for i, arg := range command {
			command[i] = strings.ReplaceAll(arg, "set -xe;", "")
		}
		fmt.Fprintf(output, "Executing command:\n%s\n\n", strings.TrimSpace(strings.Join(command, " ")))
	}
	cmdArgs := step.Entrypoint[1:]
	cmdArgs = append(cmdArgs, step.Command...)

	cmd := exec.Command(step.Entrypoint[0], cmdArgs...) //nolint:gosec
	cmd.Dir = step.WorkingDir
	cmd.Env = toEnv(step.Envs)
	cmd.Stderr = output
	cmd.Stdout = output

	if err := cmd.Start(); err != nil {
		return nil, err
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
