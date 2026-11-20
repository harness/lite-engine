// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package sidecar

import (
	"fmt"
	"unsafe"

	"golang.org/x/sys/unix"
)

// setProcessName sets the name shown by ps. Linux keeps 15 characters of it.
func setProcessName(name string) error {
	const maxName = 15
	if name == "" || len(name) > maxName {
		return fmt.Errorf("process name must be 1 to %d characters", maxName)
	}
	var buf [maxName + 1]byte
	copy(buf[:], name)
	// The kernel reads a NUL-terminated string from this pointer.
	return unix.Prctl(unix.PR_SET_NAME, uintptr(unsafe.Pointer(&buf[0])), 0, 0, 0) //nolint:gosec // the buffer outlives the call
}
