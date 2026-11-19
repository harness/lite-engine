// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"testing"
)

func TestChunkParserStopsBeforePipelinedRequest(t *testing.T) {
	body := []byte("5\r\nhello\r\n0\r\n\r\nGET /next HTTP/1.1\r\n\r\n")
	var p chunkParser
	n, done := p.consume(body)
	if !done || p.malformed {
		t.Fatalf("done=%v malformed=%v", done, p.malformed)
	}
	if got := string(body[:n]); got != "5\r\nhello\r\n0\r\n\r\n" {
		t.Fatalf("consumed %q", got)
	}
}

func TestChunkParserResumesAcrossReads(t *testing.T) {
	var p chunkParser
	n, done := p.consume([]byte("5\r\nhel"))
	if done || n != len("5\r\nhel") {
		t.Fatalf("first consume n=%d done=%v", n, done)
	}
	rest := []byte("lo\r\n0\r\n\r\nGET /next")
	n, done = p.consume(rest)
	if !done || string(rest[:n]) != "lo\r\n0\r\n\r\n" {
		t.Fatalf("second consume n=%d done=%v %q", n, done, rest[:n])
	}
}

// A CRLF that straddles two reads must still end the size line.
func TestChunkParserCRLFSplitAcrossReads(t *testing.T) {
	for _, parts := range [][]string{
		{"5\r", "\nhello\r\n0\r\n\r\n"},
		{"5", "\r\nhello\r", "\n0\r\n\r\n"},
		{"5\r\nhello\r\n0\r\n\r", "\n"},
		{"5\r\nhello\r\n0\r\nX-T: v\r", "\n\r\n"},
	} {
		var p chunkParser
		var done bool
		total := 0
		for _, part := range parts {
			var n int
			n, done = p.consume([]byte(part))
			total += n
		}
		if !done || p.malformed {
			t.Fatalf("parts %q: done=%v malformed=%v", parts, done, p.malformed)
		}
		if want := len(bytes.Join(stringsToBytes(parts), nil)); total != want {
			t.Fatalf("parts %q: consumed %d, want %d", parts, total, want)
		}
	}
}

func stringsToBytes(parts []string) [][]byte {
	out := make([][]byte, len(parts))
	for i, p := range parts {
		out[i] = []byte(p)
	}
	return out
}

func TestChunkParserTrailers(t *testing.T) {
	body := []byte("3\r\nabc\r\n0\r\nX-Sum: 1\r\n\r\nEXTRA")
	var p chunkParser
	n, done := p.consume(body)
	if !done || string(body[:n]) != "3\r\nabc\r\n0\r\nX-Sum: 1\r\n\r\n" {
		t.Fatalf("n=%d done=%v", n, done)
	}
}

// A hostile chunk size must not overflow into a negative slice index.
func TestChunkParserHostileSizesAreMalformedNotPanics(t *testing.T) {
	for _, size := range []string{
		"7fffffffffffffff", "ffffffffffffffff", "80000000", "7fffffff", "ffffffff", "-1", "+5x", "zz", "",
	} {
		var p chunkParser
		_, done := p.consume([]byte(size + "\r\nhello\r\n"))
		if !done || !p.malformed {
			t.Fatalf("size %q: done=%v malformed=%v", size, done, p.malformed)
		}
	}
}

func TestChunkParserOverlongLineIsMalformed(t *testing.T) {
	var p chunkParser
	_, done := p.consume(bytes.Repeat([]byte("a"), maxChunkLine+1))
	if !done || !p.malformed {
		t.Fatalf("done=%v malformed=%v", done, p.malformed)
	}
}

func TestParseChunkSizeExtension(t *testing.T) {
	n, ok := parseChunkSize([]byte("5;ext=1"))
	if !ok || n != 5 {
		t.Fatalf("size = %d, %v", n, ok)
	}
	if _, ok := parseChunkSize([]byte("zz")); ok {
		t.Fatal("a malformed size was accepted")
	}
}

func TestContentLengthAndChunkedHeaders(t *testing.T) {
	head := []byte("POST / HTTP/1.1\r\nHost: example.com\r\nContent-Length: 4\r\n\r\n")
	if n, ok := contentLength(head); !ok || n != 4 {
		t.Fatalf("contentLength = %d, %v", n, ok)
	}
	if isChunked(head) {
		t.Fatal("isChunked = true for a Content-Length request")
	}
	if !isChunked([]byte("POST / HTTP/1.1\r\nTransfer-Encoding: gzip, chunked\r\n\r\n")) {
		t.Fatal("isChunked = false")
	}
	if got, ok := headerValue([]byte("GET / HTTP/1.1\r\nHost: Example.COM\r\n\r\n"), "host"); !ok || got != "Example.COM" {
		t.Fatalf("host = %q, %v", got, ok)
	}
}

func TestLogHostDropsPathQueryAndUserinfo(t *testing.T) {
	for in, want := range map[string]string{
		"http://example.com/a/b?token=x":    "example.com",
		"http://user:pw@example.com:8080/x": "example.com:8080",
		"example.com:443":                   "example.com:443",
		"https://example.com?x=1#frag":      "example.com",
		"/relative/path?token=x":            "",
		"":                                  "",
		"http://" + string(bytes.Repeat([]byte("h"), 200)): string(bytes.Repeat([]byte("h"), maxLogHost)),
	} {
		if got := logHost(in); got != want {
			t.Fatalf("logHost(%q) = %q, want %q", in, got, want)
		}
	}
}
