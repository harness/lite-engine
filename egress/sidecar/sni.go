// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import "encoding/binary"

const (
	clientHelloType = 1
	// minClientHelloLen is the shortest handshake body that can hold the
	// fixed ClientHello fields.
	minClientHelloLen = 42
	// clientHelloFixedLen is the handshake type (1), length (3), version (2),
	// and random (32).
	clientHelloFixedLen = 38

	uint8Width  = 1
	uint16Width = 2

	extHeaderLen   = 4
	serverNameExt  = 0
	serverNameHdr  = 5 // list length (2), name type (1), name length (2)
	maxHostnameLen = 253
)

// sniFromClientHello returns the server name from a TLS ClientHello record, or
// "" when there is none, the record is incomplete or malformed, or the name is
// not a plausible host name. It never returns a partial name: a length field
// that runs past the bytes received yields "", and the caller falls back to
// the original destination.
func sniFromClientHello(buf []byte) string {
	exts, ok := clientHelloExtensions(buf)
	if !ok {
		return ""
	}
	return serverNameFromExtensions(exts)
}

func clientHelloExtensions(buf []byte) ([]byte, bool) {
	if len(buf) < tlsRecordHeaderLen || buf[0] != tlsHandshakeRecord {
		return nil, false
	}
	recLen := int(binary.BigEndian.Uint16(buf[3:tlsRecordHeaderLen]))
	if tlsRecordHeaderLen+recLen > len(buf) {
		return nil, false // record not fully received
	}
	hs := buf[tlsRecordHeaderLen : tlsRecordHeaderLen+recLen]
	if len(hs) < minClientHelloLen || hs[0] != clientHelloType {
		return nil, false
	}
	i := clientHelloFixedLen
	var ok bool
	for _, width := range []int{uint8Width, uint16Width, uint8Width} { // session id, cipher suites, compression
		if i, ok = skipVector(hs, i, width); !ok {
			return nil, false
		}
	}
	if i+uint16Width > len(hs) {
		return nil, false
	}
	total := int(binary.BigEndian.Uint16(hs[i : i+uint16Width]))
	i += uint16Width
	if i+total > len(hs) {
		return nil, false // extensions run past the record
	}
	return hs[i : i+total], true
}

// skipVector steps over a length-prefixed vector whose length field is width
// bytes wide. It reports false when the vector runs past b.
func skipVector(b []byte, i, width int) (int, bool) {
	if i+width > len(b) {
		return 0, false
	}
	n := int(b[i])
	if width == uint16Width {
		n = int(binary.BigEndian.Uint16(b[i : i+uint16Width]))
	}
	next := i + width + n
	if next > len(b) {
		return 0, false
	}
	return next, true
}

func serverNameFromExtensions(exts []byte) string {
	for i := 0; i+extHeaderLen <= len(exts); {
		etype := binary.BigEndian.Uint16(exts[i : i+uint16Width])
		elen := int(binary.BigEndian.Uint16(exts[i+uint16Width : i+extHeaderLen]))
		i += extHeaderLen
		if i+elen > len(exts) {
			return "" // this extension is cut short
		}
		data := exts[i : i+elen]
		i += elen
		if etype == serverNameExt {
			return hostnameFromServerNameExt(data)
		}
	}
	return ""
}

func hostnameFromServerNameExt(data []byte) string {
	if len(data) < serverNameHdr {
		return ""
	}
	nlen := int(binary.BigEndian.Uint16(data[serverNameHdr-uint16Width : serverNameHdr]))
	if serverNameHdr+nlen > len(data) {
		return ""
	}
	name := string(data[serverNameHdr : serverNameHdr+nlen])
	if !validHostname(name) {
		return ""
	}
	return name
}

// validHostname accepts letters, digits, dot, hyphen, and underscore. The name
// is copied into a CONNECT request line and a Host header, so anything else,
// including CR, LF, space, and colon, is refused.
func validHostname(name string) bool {
	if name == "" || len(name) > maxHostnameLen {
		return false
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
		case c == '.', c == '-', c == '_':
		default:
			return false
		}
	}
	return true
}
