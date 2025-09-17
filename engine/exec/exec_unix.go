// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build unix

package exec

import (
	"context"
	"os/exec"
	"strconv"
	"syscall"
	"time"

	"github.com/sirupsen/logrus"
)

func SetSysProcAttr(cmd *exec.Cmd, userIDStr string, useNewProcessGroup bool) {
	if userIDStr == "" && !useNewProcessGroup {
		return
	}
	sysProcAttr := &syscall.SysProcAttr{}
	if userIDStr != "" {
		if userID, err := strconv.Atoi(userIDStr); err == nil {
			sysProcAttr.Credential = &syscall.Credential{Uid: uint32(userID)}
		}
	}
	if useNewProcessGroup {
		sysProcAttr.Setpgid = true
	}
	cmd.SysProcAttr = sysProcAttr
}

// AbortProcess attempts to gracefully terminate the process group of `cmd`
// by sending SIGTERM to the process group, and escalates to SIGKILL after a timeout
// if the process has not exited. The function relies on the cmdSignal channel to detect
// whether the process has already terminated.
func AbortProcess(ctx context.Context, cmd *exec.Cmd, cmdSignal <-chan cmdResult) {
	if cmd.Process == nil {
		logrus.WithContext(ctx).Warnln("abort requested but cmd.Process is nil")
		return
	}

	pgid := -cmd.Process.Pid // negative PID = process group
	gracefulAbortTimeout := 1 * time.Minute

	// Start escalation goroutine: if the process group hasn't exited within the timeout,
	// escalate to SIGKILL.
	go func() {
		select {
		case <-time.After(gracefulAbortTimeout):
			// If `gracefulAbortTimeout` has passed, we escalate to SIGKILL
			logrus.WithContext(ctx).Warnf("timeout reached (%s). Escalating to SIGKILL for process group %d", gracefulAbortTimeout, pgid)
			if err := syscall.Kill(pgid, syscall.SIGKILL); err != nil {
				logrus.WithContext(ctx).WithError(err).Errorf("failed to send SIGKILL to process group %d", pgid)
			}
			return
		case <-cmdSignal:
			// If subprocess exits, we are good here
			logrus.WithContext(ctx).Debugf("process group %d exited", pgid)
			return
		}
	}()

	// Attempt to kill the process group gracefully, using SIGTERM
	logrus.WithContext(ctx).Debugf("sending SIGTERM to process group %d", pgid)
	if err := syscall.Kill(pgid, syscall.SIGTERM); err != nil {
		logrus.WithContext(ctx).WithError(err).Errorf("failed to send SIGTERM to process group %d", pgid)
	}
}
