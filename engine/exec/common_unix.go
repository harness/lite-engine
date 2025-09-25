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

// AbortProcessGroup attempts to gracefully terminate the process group `pgid`
// by sending SIGTERM to the process group, and escalates to SIGKILL after a timeout
// if the process has not exited. The function relies on the cmdSignal channel to detect
// whether the process has already terminated.
func abortProcessGroup(ctx context.Context, pgid int, retryIntervalSecs int, maxSigtermAttempts int, cmdSignal <-chan cmdResult) {
	// Default process abort strategy is to send SIGTERM to the step's process group;
	// We send it a a number of times (`maxSigtermAttempts`) in intervals of (`retryIntervalSecs`).
	// If process group still has not exited after these, we escalate to SIGKILL.
	if maxSigtermAttempts <= 0 {
		sendKillSignalToProcessGroup(ctx, pgid, "SIGKILL", syscall.SIGKILL)
		return
	}
	// First attempt to kill process group gracefully
	sendKillSignalToProcessGroup(ctx, pgid, "SIGTERM", syscall.SIGTERM)
	sigtermAttempts := 1
	retryInterval := time.Duration(retryIntervalSecs) * time.Second
	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ticker.C:
			sigtermAttempts++
			if sigtermAttempts <= maxSigtermAttempts {
				logrus.WithContext(ctx).Warnf("process group %d shutdown retry interval reached (%ds). Retrying graceful shutdown (attempt %d of %d)", pgid, retryIntervalSecs, sigtermAttempts, maxSigtermAttempts)
				sendKillSignalToProcessGroup(ctx, pgid, "SIGTERM", syscall.SIGTERM)
			} else {
				// max graceful shutdown attemtps reached. Escalating to SIGKILL
				logrus.WithContext(ctx).Warnf("process group %d did not shutdown with SIGTERM after %d attempts. Escalating to SIGKILL", pgid, maxSigtermAttempts)
				sendKillSignalToProcessGroup(ctx, pgid, "SIGKILL", syscall.SIGKILL)
				return
			}
		case <-cmdSignal:
			// If subprocess exits, we are good here
			logrus.WithContext(ctx).Debugf("process group %d exited", pgid)
			return
		}
	}
}

func sendKillSignalToProcessGroup(ctx context.Context, pgid int, signalName string, signalValue syscall.Signal) {
	logrus.WithContext(ctx).Debugf("sending %s to process group %d", signalName, pgid)
	if err := syscall.Kill(-pgid, signalValue); err != nil { // negative PID = process group
		logrus.WithContext(ctx).WithError(err).Errorf("failed to send %s to process group %d", signalName, pgid)
	}
}
