// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build unix

package exec

import (
	"os/exec"
	"strconv"
	"syscall"
)

func SetSysProcAttr(cmd *exec.Cmd, userIDStr string, setPgid bool) {
	if userIDStr == "" && !setPgid {
		return
	}
	sysProcAttr := &syscall.SysProcAttr{}
	if userIDStr != "" {
		if userID, err := strconv.Atoi(userIDStr); err == nil {
			sysProcAttr.Credential = &syscall.Credential{Uid: uint32(userID)}
		}
	}
	if setPgid {
		sysProcAttr.Setpgid = true
	}
	cmd.SysProcAttr = sysProcAttr
}
