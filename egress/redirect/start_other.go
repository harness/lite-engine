// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build !linux

package redirect

import (
	"errors"
	"syscall"
)

// DialControl is a no-op off Linux, where SO_MARK does not exist. The redirect
// itself is not installed off Linux.
func DialControl(string, string, syscall.RawConn) error { return nil }

func startSidecar(string, int, int, int) (int, error) {
	return 0, errors.New("egress sidecar redirect is only supported on linux")
}

func stopSidecar() {}

func sidecarAlive(int) bool { return false }

func sidecarListening(int) bool { return false }

func defaultInterface() (string, error) {
	return "", errors.New("egress sidecar redirect is only supported on linux")
}
