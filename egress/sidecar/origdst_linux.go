//go:build linux

package sidecar

import (
	"encoding/binary"
	"fmt"
	"net"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// soOriginalDst is SO_ORIGINAL_DST from linux/netfilter_ipv4.h. It is the
// address iptables REDIRECT rewrote, which is the address the client dialed.
const soOriginalDst = 80

// OriginalDst returns the IPv4 address and port the client connected to before
// the redirect. IPv6 is out of scope for this release.
func OriginalDst(conn net.Conn) (string, int, error) {
	sc, ok := conn.(syscall.Conn)
	if !ok {
		return "", 0, fmt.Errorf("connection does not expose a socket")
	}
	raw, err := sc.SyscallConn()
	if err != nil {
		return "", 0, err
	}
	var ip string
	var port int
	var ctrlErr error
	if err := raw.Control(func(fd uintptr) {
		var buf [16]byte
		optlen := uint32(len(buf))
		_, _, errno := unix.Syscall6(
			unix.SYS_GETSOCKOPT,
			fd,
			uintptr(unix.SOL_IP),
			soOriginalDst,
			uintptr(unsafe.Pointer(&buf[0])),
			uintptr(unsafe.Pointer(&optlen)),
			0,
		)
		if errno != 0 {
			ctrlErr = errno
			return
		}
		// sockaddr_in: family(2) port(2, network order) addr(4). Same layout the
		// POC unpacked with !HHBBBB.
		port = int(binary.BigEndian.Uint16(buf[2:4]))
		ip = net.IPv4(buf[4], buf[5], buf[6], buf[7]).String()
	}); err != nil {
		return "", 0, err
	}
	if ctrlErr != nil {
		return "", 0, ctrlErr
	}
	return ip, port, nil
}
