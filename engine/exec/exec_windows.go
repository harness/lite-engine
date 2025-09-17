// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build windows

package exec

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"

	"github.com/sirupsen/logrus"
)

func SetSysProcAttr(cmd *exec.Cmd, userIDStr string, useNewProcessGroup bool) {
	if useNewProcessGroup {
		cmd.SysProcAttr = &syscall.SysProcAttr{
			CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP,
		}
	}
}

// AbortProcess leverages taskkill.exe in Windows to make subprocesses exit.
// Here we use the following flags:
// /t (tree) -> also terminate all child processes spawned by the PID (recursively)
// /f (force) -> forcibly terminates the process
func AbortProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal <-chan cmdResult) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("abort requested but cmd.Process is nil")
		return
	}
	pid := cmd.Process.Pid
	logrus.WithContext(ctx).Debugf("invoking taskkill.exe /t /f /pid %d", pid)
	killCmd := exec.Command("taskkill.exe", "/t", "/f", "/pid", strconv.Itoa(pid))
	err := killCmd.Run()
	if err != nil {
		logrus.WithContext(ctx).WithError(err).Errorf("failed to invoke taskkill.exe for process %d", pid)
	}
}
