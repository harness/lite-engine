//go:build !linux

package sidecar

import (
	"fmt"
	"net"
)

// OriginalDst reports the pre-redirect destination. It exists only on Linux,
// where the hosted VM applies the iptables redirect.
func OriginalDst(conn net.Conn) (string, int, error) {
	return "", 0, fmt.Errorf("SO_ORIGINAL_DST is only available on linux")
}
