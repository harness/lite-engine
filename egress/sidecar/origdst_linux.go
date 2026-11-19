// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package sidecar

import (
	"encoding/binary"
	"errors"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

var errNoSocket = errors.New("connection does not expose a socket")

// soOriginalDst is SO_ORIGINAL_DST from linux/netfilter_ipv4.h. It returns the
// address iptables REDIRECT rewrote, which is the address the client dialed.
const soOriginalDst = 80

// Offsets inside the sockaddr_in that SO_ORIGINAL_DST fills in: family (2),
// port (2, network order), address (4).
const (
	sockaddrPortOffset = 2
	sockaddrAddrOffset = 4
)

// originalDst returns the IPv4 address and port the client connected to before
// the redirect. IPv6 is out of scope for this release.
func originalDst(conn net.Conn) (ip string, port int, err error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return "", 0, errNoSocket
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return "", 0, err
	}
	var mreq *unix.IPv6Mreq
	var sockErr error
	if err := raw.Control(func(fd uintptr) {
		// The 16-byte sockaddr_in fits in IPv6Mreq.Multiaddr. This is the
		// usual way to call SO_ORIGINAL_DST without unsafe.
		mreq, sockErr = unix.GetsockoptIPv6Mreq(int(fd), unix.SOL_IP, soOriginalDst)
	}); err != nil {
		return "", 0, err
	}
	if sockErr != nil {
		return "", 0, sockErr
	}
	m := mreq.Multiaddr
	port = int(binary.BigEndian.Uint16(m[sockaddrPortOffset : sockaddrPortOffset+2]))
	ip = net.IP(m[sockaddrAddrOffset : sockaddrAddrOffset+net.IPv4len]).String()
	return ip, port, nil
}
