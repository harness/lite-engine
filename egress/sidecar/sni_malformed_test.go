// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"encoding/binary"
	"strings"
	"testing"
)

// recordOf wraps handshake bytes in a TLS record whose length field is correct
// for them, so the record is complete and only the handshake inside is wrong.
func recordOf(t testing.TB, hs []byte) []byte {
	t.Helper()
	rec := binary.BigEndian.AppendUint16([]byte{tlsHandshakeRecord, 0x03, 0x01}, u16(t, len(hs)))
	return append(rec, hs...)
}

// Every shorter handshake is a complete record with a ClientHello cut off
// inside one of its length-prefixed fields. None of them may produce a name.
func TestSNIOfEveryTruncatedHandshakeIsEmpty(t *testing.T) {
	full := buildClientHello(t, "pypi.org")
	hs := full[tlsRecordHeaderLen:]
	for n := 0; n < len(hs); n++ {
		if got := sniFromClientHello(recordOf(t, hs[:n])); got != "" {
			t.Fatalf("SNI of a handshake cut to %d of %d bytes = %q, want empty", n, len(hs), got)
		}
	}
	if got := sniFromClientHello(recordOf(t, hs)); got != "pypi.org" {
		t.Fatalf("SNI of the whole handshake = %q", got)
	}
}

func TestSNIRefusesAHandshakeThatIsNotAClientHello(t *testing.T) {
	hs := buildClientHello(t, "pypi.org")[tlsRecordHeaderLen:]
	hs[0] = 2 // ServerHello
	if got := sniFromClientHello(recordOf(t, hs)); got != "" {
		t.Fatalf("SNI = %q, want empty", got)
	}
}

func TestSNIMalformedExtensions(t *testing.T) {
	validName := serverNameExtension(t, "pypi.org")
	// An extension ahead of server_name is skipped, not mistaken for it.
	skipped := append([]byte{0x00, 0x10, 0x00, 0x03, 'a', 'b', 'c'}, validName...)

	tests := []struct {
		name string
		exts []byte
		want string
	}{
		{name: "another extension comes first", exts: skipped, want: "pypi.org"},
		{name: "extension runs past the extensions", exts: []byte{0x00, 0x00, 0x01, 0x00, 'a', 'b'}},
		{name: "extension header is cut short", exts: []byte{0x00, 0x00, 0x00}},
		{name: "server_name data is too short for its header", exts: []byte{0x00, 0x00, 0x00, 0x02, 0x00, 0x00}},
		{
			name: "server name runs past the extension",
			exts: []byte{0x00, 0x00, 0x00, 0x08, 0x00, 0x06, 0x00, 0x00, 0x32, 'a', 'b', 'c'},
		},
		{name: "server name is empty", exts: []byte{0x00, 0x00, 0x00, 0x05, 0x00, 0x03, 0x00, 0x00, 0x00}},
		{name: "server name is too long", exts: serverNameExtension(t, strings.Repeat("a", maxHostnameLen+1))},
		{name: "server name is the longest allowed", exts: serverNameExtension(t, strings.Repeat("a", maxHostnameLen)), want: strings.Repeat("a", maxHostnameLen)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := sniFromClientHello(helloWithExtensions(t, tt.exts)); got != tt.want {
				t.Fatalf("SNI = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestSNIRefusesAHandshakeShorterThanAClientHello(t *testing.T) {
	if got := sniFromClientHello(recordOf(t, []byte{clientHelloType, 0, 0, 2, 3, 3})); got != "" {
		t.Fatalf("SNI = %q, want empty", got)
	}
}
