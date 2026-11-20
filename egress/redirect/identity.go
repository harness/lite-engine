// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"bytes"

	"github.com/harness/lite-engine/egress/sidecar"
)

// isSidecarArgs reports whether cmdline, the NUL-separated argument list from
// /proc/<pid>/cmdline, is the egress sidecar. The kernel records it at exec, so
// it identifies the process before the process sets its own name. The parent
// lite-engine command does not carry this argument.
func isSidecarArgs(cmdline []byte) bool {
	if len(cmdline) == 0 {
		return false
	}
	if cmdline[len(cmdline)-1] == 0 {
		cmdline = cmdline[:len(cmdline)-1]
	}
	for _, arg := range bytes.Split(cmdline, []byte{0}) {
		if string(arg) == sidecar.ProcessName {
			return true
		}
	}
	return false
}
