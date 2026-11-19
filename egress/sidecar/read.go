// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"time"
)

// httpMethods are the request methods the sidecar waits for a full head on.
var httpMethods = [][]byte{
	[]byte("GET"), []byte("HEAD"), []byte("POST"), []byte("PUT"), []byte("DELETE"),
	[]byte("CONNECT"), []byte("OPTIONS"), []byte("TRACE"), []byte("PATCH"),
	[]byte("PROPFIND"), []byte("PROPPATCH"), []byte("MKCOL"), []byte("COPY"),
	[]byte("MOVE"), []byte("LOCK"), []byte("UNLOCK"), []byte("REPORT"), []byte("SEARCH"),
}

// readFirst reads the bytes needed to decide how to relay one connection: one
// TLS record, or the HTTP request head. A timeout or EOF with some bytes
// already read returns those bytes.
func readFirst(conn net.Conn, wait time.Duration) ([]byte, error) {
	if wait > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(wait))
		defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	}
	buf := make([]byte, 0, readChunkSize)
	tmp := make([]byte, readChunkSize)
	for len(buf) < maxFirstRead {
		n, err := conn.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if n > 0 && firstReadDone(buf) {
			return buf, nil
		}
		if err != nil {
			if len(buf) > 0 && (isTimeout(err) || errors.Is(err, io.EOF)) {
				return buf, nil
			}
			return buf, err
		}
	}
	return buf, nil
}

func firstReadDone(buf []byte) bool {
	if buf[0] == tlsHandshakeRecord {
		return tlsRecordComplete(buf)
	}
	return bytes.Contains(buf, crlfcrlf) || !looksLikeHTTP(buf)
}

func tlsRecordComplete(buf []byte) bool {
	if len(buf) < tlsRecordHeaderLen {
		return false
	}
	return len(buf) >= tlsRecordHeaderLen+int(binary.BigEndian.Uint16(buf[3:tlsRecordHeaderLen]))
}

// looksLikeHTTP reports whether buf is, or could still grow into, the start of
// an HTTP request line. A protocol that is not HTTP, such as SSH on port 443,
// is relayed at once instead of waiting for a blank line that never comes.
func looksLikeHTTP(buf []byte) bool {
	for _, method := range httpMethods {
		if len(buf) <= len(method) {
			if bytes.HasPrefix(method, buf) {
				return true
			}
			continue
		}
		if bytes.HasPrefix(buf, method) && buf[len(method)] == ' ' {
			return true
		}
	}
	return false
}
