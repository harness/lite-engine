// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build unix

package exec

import (
	"os/exec"
	"syscall"
)

func SetUserID(cmd *exec.Cmd, userID uint32) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Credential: &syscall.Credential{
			Uid: userID,
		},
	}
}
