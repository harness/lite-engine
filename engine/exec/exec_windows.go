// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build windows

package exec

import (
	"os/exec"
)

func SetSysProcAttr(cmd *exec.Cmd, userIDStr string) {
	// This is a no-op on Windows
}
