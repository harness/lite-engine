package sidecar

import (
	"bytes"
	"testing"
)

func TestChunkParserStopsBeforePipelinedRequest(t *testing.T) {
	body := []byte("5\r\nhello\r\n0\r\n\r\nGET /next HTTP/1.1\r\n\r\n")
	var p chunkParser
	n, done := p.consume(body)
	if !done {
		t.Fatal("done = false")
	}
	if got := string(body[:n]); got != "5\r\nhello\r\n0\r\n\r\n" {
		t.Fatalf("consumed %q", got)
	}
}

func TestChunkParserResumeAcrossReads(t *testing.T) {
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

func TestContentLengthIgnoresPipelinedTail(t *testing.T) {
	head := []byte("POST / HTTP/1.1\r\nHost: example.com\r\nContent-Length: 4")
	n, ok := contentLength(head)
	if !ok || n != 4 {
		t.Fatalf("contentLength = %d, %v", n, ok)
	}
	if chunked(head) {
		t.Fatal("chunked = true")
	}
}

func TestChunkedHeader(t *testing.T) {
	head := []byte("POST / HTTP/1.1\r\nTransfer-Encoding: gzip, chunked")
	if !chunked(head) {
		t.Fatal("chunked = false")
	}
}

func TestParseChunkSizeExtension(t *testing.T) {
	n, ok := parseChunkSize([]byte("5;ext=1"))
	if !ok || n != 5 {
		t.Fatalf("size = %d, %v", n, ok)
	}
	if _, ok := parseChunkSize([]byte("zz")); ok {
		t.Fatal("malformed size was accepted")
	}
}

func TestHeaderValueIsCaseInsensitive(t *testing.T) {
	got, ok := headerValue([]byte("GET / HTTP/1.1\r\nHost: Example.COM"), "host")
	if !ok || got != "Example.COM" {
		t.Fatalf("host = %q, %v", got, ok)
	}
	if bytes.Contains([]byte(got), []byte("\r")) {
		t.Fatalf("host still has CR: %q", got)
	}
}
