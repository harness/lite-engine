// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"encoding/binary"
	"math"
	"testing"
)

func u16(t testing.TB, n int) uint16 {
	t.Helper()
	if n < 0 || n > math.MaxUint16 {
		t.Fatalf("%d does not fit in 16 bits", n)
	}
	return uint16(n)
}

// buildClientHello builds a TLS ClientHello record. An empty name omits the SNI
// extension.
func buildClientHello(t testing.TB, name string) []byte {
	t.Helper()
	var exts []byte
	if name != "" {
		entry := binary.BigEndian.AppendUint16([]byte{0}, u16(t, len(name)))
		entry = append(entry, name...)
		list := binary.BigEndian.AppendUint16(nil, u16(t, len(entry)))
		list = append(list, entry...)
		exts = binary.BigEndian.AppendUint16(exts, 0) // server_name
		exts = binary.BigEndian.AppendUint16(exts, u16(t, len(list)))
		exts = append(exts, list...)
	}

	body := binary.BigEndian.AppendUint16(nil, 0x0303) // version
	body = append(body, make([]byte, 32)...)           // random
	body = append(body, 0)                             // session id length
	body = binary.BigEndian.AppendUint16(body, 2)      // cipher suites length
	body = append(body, 0x00, 0x2f, 1, 0)              // one suite, one compression method
	body = binary.BigEndian.AppendUint16(body, u16(t, len(exts)))
	body = append(body, exts...)

	hs := []byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	hs = append(hs, body...)

	rec := binary.BigEndian.AppendUint16([]byte{22, 0x03, 0x01}, u16(t, len(hs)))
	return append(rec, hs...)
}

func TestSNIReadsServerName(t *testing.T) {
	if got := sniFromClientHello(buildClientHello(t, "pypi.org")); got != "pypi.org" {
		t.Fatalf("SNI = %q, want pypi.org", got)
	}
}

func TestSNIWithoutExtensionIsEmpty(t *testing.T) {
	if got := sniFromClientHello(buildClientHello(t, "")); got != "" {
		t.Fatalf("SNI = %q, want empty", got)
	}
}

func TestSNIRejectsNonHandshake(t *testing.T) {
	if got := sniFromClientHello([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")); got != "" {
		t.Fatalf("SNI = %q, want empty for plain HTTP", got)
	}
}

func TestSNITruncatedIsEmpty(t *testing.T) {
	full := buildClientHello(t, "example.com")
	for _, n := range []int{0, 3, 5, 20, 42, len(full) - 3} {
		if got := sniFromClientHello(full[:n]); got != "" {
			t.Fatalf("SNI of %d-byte prefix = %q, want empty", n, got)
		}
	}
}

// The name is copied into a CONNECT line and a Host header, so a name that is
// not a plausible host name is refused.
func TestSNIRejectsImplausibleNames(t *testing.T) {
	for _, name := range []string{
		"evil.example\r\nX-Injected: yes",
		"has space.example",
		"host:443",
		"slash/path.example",
		"nul\x00byte.example",
	} {
		if got := sniFromClientHello(buildClientHello(t, name)); got != "" {
			t.Fatalf("SNI for %q = %q, want empty", name, got)
		}
	}
	for _, name := range []string{"a.b-c.example.com", "under_score.example", "UPPER.Example.COM", "1.2.3.4"} {
		if got := sniFromClientHello(buildClientHello(t, name)); got != name {
			t.Fatalf("SNI for %q = %q", name, got)
		}
	}
}
