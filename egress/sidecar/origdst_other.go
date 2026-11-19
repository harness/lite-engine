// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build !linux

package sidecar

import (
	"errors"
	"net"
)

// originalDst reports the pre-redirect destination. It exists only on Linux,
// where the hosted VM applies the iptables redirect.
func originalDst(_ net.Conn) (ip string, port int, err error) {
	return "", 0, errors.New("SO_ORIGINAL_DST is only available on linux")
}
