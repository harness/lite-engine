package sidecar

import (
	"bytes"
	"encoding/binary"
	"io"
	"net"
	"time"
)

// ReadFirst reads the bytes needed to decide how to relay one connection: one
// TLS record when the server name is already present, or the HTTP head.
// A timeout with some bytes already read returns those bytes.
func ReadFirst(conn net.Conn, wait time.Duration) ([]byte, error) {
	if wait > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		defer conn.SetReadDeadline(time.Time{})
	}
	buf := make([]byte, 0, 4096)
	tmp := make([]byte, 4096)
	for len(buf) < 16384 {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
			if firstReadDone(buf) {
				return buf, nil
			}
		}
		if err != nil {
			if len(buf) > 0 && (isTimeout(err) || err == io.EOF) {
				return buf, nil
			}
			return buf, err
		}
	}
	return buf, nil
}

func firstReadDone(buf []byte) bool {
	if len(buf) == 0 {
		return false
	}
	if buf[0] == 22 {
		if len(buf) < 5 {
			return false
		}
		need := 5 + int(binary.BigEndian.Uint16(buf[3:5]))
		return len(buf) >= need || SNIFromClientHello(buf) != ""
	}
	if bytes.Contains(buf, crlfcrlf) {
		return true
	}
	switch buf[0] {
	case 'G', 'P', 'C', 'H', 'D', 'O', 'T':
		return false
	default:
		return true
	}
}
