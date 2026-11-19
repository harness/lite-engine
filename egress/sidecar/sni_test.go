package sidecar

import (
	"encoding/binary"
	"testing"
)

// buildClientHello builds a TLS ClientHello record. An empty name omits the SNI
// extension.
func buildClientHello(name string) []byte {
	var exts []byte
	if name != "" {
		entry := []byte{0}
		entry = binary.BigEndian.AppendUint16(entry, uint16(len(name)))
		entry = append(entry, name...)
		list := binary.BigEndian.AppendUint16(nil, uint16(len(entry)))
		list = append(list, entry...)
		exts = binary.BigEndian.AppendUint16(exts, 0) // server_name
		exts = binary.BigEndian.AppendUint16(exts, uint16(len(list)))
		exts = append(exts, list...)
	}

	var body []byte
	body = binary.BigEndian.AppendUint16(body, 0x0303) // version
	body = append(body, make([]byte, 32)...)           // random
	body = append(body, 0)                             // session id length
	body = binary.BigEndian.AppendUint16(body, 2)      // cipher suites length
	body = append(body, 0x00, 0x2f)
	body = append(body, 1, 0) // compression methods
	body = binary.BigEndian.AppendUint16(body, uint16(len(exts)))
	body = append(body, exts...)

	hs := []byte{1, byte(len(body) >> 16), byte(len(body) >> 8), byte(len(body))}
	hs = append(hs, body...)

	rec := []byte{22, 0x03, 0x01}
	rec = binary.BigEndian.AppendUint16(rec, uint16(len(hs)))
	return append(rec, hs...)
}

func TestSNIFromClientHelloReadsServerName(t *testing.T) {
	if got := SNIFromClientHello(buildClientHello("pypi.org")); got != "pypi.org" {
		t.Fatalf("SNI = %q, want pypi.org", got)
	}
}

func TestSNIFromClientHelloWithoutExtensionIsEmpty(t *testing.T) {
	if got := SNIFromClientHello(buildClientHello("")); got != "" {
		t.Fatalf("SNI = %q, want empty", got)
	}
}

func TestSNIFromClientHelloRejectsNonHandshake(t *testing.T) {
	if got := SNIFromClientHello([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n")); got != "" {
		t.Fatalf("SNI = %q, want empty for plain HTTP", got)
	}
}

func TestSNIFromClientHelloTruncatedIsEmpty(t *testing.T) {
	full := buildClientHello("example.com")
	for _, n := range []int{5, 20, 42, len(full) - 3} {
		if got := SNIFromClientHello(full[:n]); got != "" {
			t.Fatalf("SNI of %d-byte prefix = %q, want empty", n, got)
		}
	}
}
