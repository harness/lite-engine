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
	"time"

	pruntime "github.com/drone/runner-go/pipeline/runtime"
	"github.com/harness/lite-engine/engine/spec"
	"github.com/harness/lite-engine/internal/safego"
	"github.com/sirupsen/logrus"
)

type cmdResult struct {
	state *pruntime.State
	err   error
}

func Run(ctx context.Context, step *spec.Step, output io.Writer) (*pruntime.State, error) {
	if len(step.Entrypoint) == 0 {
		return nil, errors.New("step entrypoint cannot be empty")
	}

	cmdArgs := step.Entrypoint[1:]
	cmdArgs = append(cmdArgs, step.Command...)

	cmd := exec.Command(step.Entrypoint[0], cmdArgs...) //nolint:gosec,noctx // nosemgrep

	SetSysProcAttr(cmd, step.User, step.ProcessConfig.KillProcessOnContextCancel)

	cmd.Dir = step.WorkingDir
	cmd.Env = spec.ToEnv(step.Envs)
	cmd.Stderr = output
	cmd.Stdout = output

	startTime := time.Now()
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Starting command on host for step %s %s", step.ID, step.Name))
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	logrus.WithContext(ctx).Infoln(fmt.Sprintf("Started command on host for step %s %s [PID: %d]", step.ID, step.Name, cmd.Process.Pid))

	cmdSignal := make(chan cmdResult, 1)
	safego.WithContext(ctx, "wait_process", func(ctx context.Context) {
		WaitForProcess(ctx, cmd, cmdSignal, step.ProcessConfig.WaitOnProcessGroup)
	})
	select {
	case <-ctx.Done():
		if step.ProcessConfig.KillProcessOnContextCancel {
			AbortProcess(ctx, cmd, cmdSignal, step.ProcessConfig.KillProcessRetryIntervalSecs,
				step.ProcessConfig.KillProcessMaxSigtermAttempts, step.ProcessConfig.KillProcessUseExplicitTreeStrategy)
		}
		if errors.Is(ctx.Err(), context.Canceled) || errors.Is(ctx.Err(), context.DeadlineExceeded) {
			logrus.WithContext(ctx).Infoln(fmt.Sprintf("Execution canceled for step %s with error %v, took %.2f seconds", step.ID, ctx.Err(), time.Since(startTime).Seconds()))
			return nil, ctx.Err()
		}
		logrus.WithContext(ctx).Infoln(fmt.Sprintf("Context of command completed for step %s with error %v, took %.2f seconds", step.ID, ctx.Err(), time.Since(startTime).Seconds()))
		return nil, fmt.Errorf("command context completed with error %v", ctx.Err())
	case result := <-cmdSignal:
		logrus.WithContext(ctx).Infoln(fmt.Sprintf("Completed command on host for step %s, took %.2f seconds", step.ID, time.Since(startTime).Seconds()))
		return result.state, result.err
	}
}
