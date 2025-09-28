// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package exec

import (
	"os/exec"

	pruntime "github.com/drone/runner-go/pipeline/runtime"
)

func waitForCmd(cmd *exec.Cmd, cmdSignal chan<- cmdResult) {
	err := cmd.Wait()
	if err == nil {
		cmdSignal <- cmdResult{state: &pruntime.State{ExitCode: 0, Exited: true}, err: nil}
		return
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		cmdSignal <- cmdResult{state: &pruntime.State{ExitCode: exitErr.ExitCode(), Exited: true}, err: nil}
		return
	}
	cmdSignal <- cmdResult{state: nil, err: err}
}
