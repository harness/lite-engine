// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build linux

package exec

import (
	"context"
	"os/exec"

	"github.com/sirupsen/logrus"
)

func WaitForProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal chan<- cmdResult, shouldWaitOnProcessGroup bool) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("wait for process requested but cmd.Process is nil")
		return
	}
	waitForCmd(cmd, cmdSignal)
}

func AbortProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal <-chan cmdResult, retryIntervalSecs, maxSigtermAttempts int, useExplicitProcessTree bool) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("abort requested but cmd.Process is nil")
		return
	}
	go abortProcessGroup(ctx, cmd.Process.Pid, retryIntervalSecs, maxSigtermAttempts, cmdSignal)
}
