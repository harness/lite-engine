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
	"strconv"
	"time"

	"github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/sirupsen/logrus"
)

type cmdResult struct {
	state *runtime.State
	err   error
}

func Run(ctx context.Context, step *spec.Step, output io.Writer) (*runtime.State, error) {
	if len(step.Entrypoint) == 0 {
		return nil, errors.New("step entrypoint cannot be empty")
	}

	cmdArgs := step.Entrypoint[1:]
	cmdArgs = append(cmdArgs, step.Command...)

	cmd := exec.CommandContext(ctx, step.Entrypoint[0], cmdArgs...) //nolint:gosec

	if step.User != "" {
		if userID, err := strconv.Atoi(step.User); err == nil {
			SetUserID(cmd, uint32(userID))
		}
	}

	cmd.Dir = step.WorkingDir
	cmd.Env = spec.ToEnv(step.Envs)
	cmd.Stderr = output
	cmd.Stdout = output

	startTime := time.Now()
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Starting command on host for step %s %s", step.ID, step.Name))
	if err := cmd.Start(); err != nil {
		return nil, err
	}

	cmdSignal := make(chan cmdResult, 1)
	go waitForCmd(cmd, cmdSignal)

	select {
	case <-ctx.Done():
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logrus.WithContext(ctx).Infoln(fmt.Sprintf("Execution canceled for step %s with error %v, took %.2f seconds", step.ID, ctx.Err(), time.Since(startTime).Seconds()))
			return nil, ctx.Err()
		} else {
			logrus.WithContext(ctx).Infoln(fmt.Sprintf("Context of command completed for step %s with error %v, took %.2f seconds", step.ID, ctx.Err(), time.Since(startTime).Seconds()))
			return nil, fmt.Errorf("command context completed with error %v", ctx.Err())
		}
	case result := <-cmdSignal:
		logrus.WithContext(ctx).Infoln(fmt.Sprintf("Completed command on host for step %s, took %.2f seconds", step.ID, time.Since(startTime).Seconds()))
		return result.state, result.err
	}
}

func waitForCmd(cmd *exec.Cmd, cmdSignal chan<- cmdResult) {
	err := cmd.Wait()
	if err == nil {
		cmdSignal <- cmdResult{state: &runtime.State{ExitCode: 0, Exited: true}, err: nil}
		return
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		cmdSignal <- cmdResult{state: &runtime.State{ExitCode: exitErr.ExitCode(), Exited: true}, err: nil}
		return
	}
	cmdSignal <- cmdResult{state: nil, err: err}
}
