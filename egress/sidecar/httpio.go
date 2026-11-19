// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"fmt"
	"net"
	"strconv"
	"strings"
	"time"
)

const (
	minStatusFields  = 2 // HTTP version and status code
	requestLineParts = 3 // method, target, and version
	schemeSepLen     = len("://")
)

// splitHead splits b after the blank line that ends an HTTP head. head
// includes that blank line.
func splitHead(b []byte) (head, rest []byte, ok bool) {
	i := bytes.Index(b, crlfcrlf)
	if i < 0 {
		return nil, nil, false
	}
	end := i + blankLineLen
	return b[:end], b[end:], true
}

func headerValue(head []byte, name string) (string, bool) {
	prefix := strings.ToLower(name) + ":"
	for _, line := range bytes.Split(head, crlf) {
		if strings.HasPrefix(strings.ToLower(string(line)), prefix) {
			return strings.TrimSpace(string(line[len(prefix):])), true
		}
	}
	return "", false
}

func contentLength(head []byte) (int, bool) {
	raw, ok := headerValue(head, "Content-Length")
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil || n < 0 {
		return 0, false
	}
	return n, true
}

func isChunked(head []byte) bool {
	raw, ok := headerValue(head, "Transfer-Encoding")
	return ok && strings.Contains(strings.ToLower(raw), "chunked")
}

// forwardRequest sends one request to upstream: the head, then exactly the body
// the head announces. Bytes after that, such as a pipelined second request, are
// not forwarded.
func forwardRequest(upstream, client net.Conn, request []byte, act *xfer) error {
	head, body, ok := splitHead(request)
	if !ok {
		return act.write(upstream, request)
	}
	if err := act.write(upstream, head); err != nil {
		return err
	}
	if isChunked(head) {
		return forwardChunked(upstream, client, body, act)
	}
	n, has := contentLength(head)
	if !has {
		return nil
	}
	return forwardFixed(upstream, client, body, n, act)
}

func forwardChunked(upstream, client net.Conn, body []byte, act *xfer) error {
	var p chunkParser
	used, done := p.consume(body)
	if p.malformed {
		return errMalformedBody
	}
	if err := act.write(upstream, body[:used]); err != nil {
		return err
	}
	buf := make([]byte, copyBufferSize)
	for !done {
		nr, err := act.read(client, buf)
		if nr > 0 {
			used, done = p.consume(buf[:nr])
			if p.malformed {
				return errMalformedBody
			}
			if werr := act.write(upstream, buf[:used]); werr != nil {
				return werr
			}
		}
		if err != nil && !done {
			return err
		}
	}
	return nil
}

func forwardFixed(upstream, client net.Conn, body []byte, n int, act *xfer) error {
	have := min(len(body), n)
	if err := act.write(upstream, body[:have]); err != nil {
		return err
	}
	return copyN(upstream, client, n-have, act)
}

func copyN(dst, src net.Conn, n int, act *xfer) error {
	buf := make([]byte, copyBufferSize)
	for n > 0 {
		nr, err := act.read(src, buf[:min(len(buf), n)])
		if nr > 0 {
			n -= nr
			if werr := act.write(dst, buf[:nr]); werr != nil {
				return werr
			}
		}
		if err != nil && n > 0 {
			return err
		}
	}
	return nil
}

// readResponseHead reads from conn until the blank line that ends a response
// head. raw holds everything read, which can include bytes after the head.
// headLen is the length of the head including the blank line, or 0 when no
// complete head arrived.
func readResponseHead(conn net.Conn, timeout time.Duration) (raw []byte, headLen int, err error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer func() { _ = conn.SetReadDeadline(time.Time{}) }()
	}
	tmp := make([]byte, readChunkSize)
	for len(raw) < maxResponseHead {
		n, rerr := conn.Read(tmp)
		raw = append(raw, tmp[:n]...)
		if i := bytes.Index(raw, crlfcrlf); i >= 0 {
			return raw, i + blankLineLen, nil
		}
		if rerr != nil {
			return raw, 0, rerr
		}
	}
	return raw, 0, errResponseHeadTooLarge
}

func firstLine(raw []byte) []byte {
	line, _, _ := bytes.Cut(raw, crlf)
	return line
}

func statusCode(line []byte) string {
	fields := bytes.Fields(line)
	if len(fields) < minStatusFields {
		return ""
	}
	return string(fields[1])
}

func connectHeader(host string, port int, credential string) []byte {
	return fmt.Appendf(nil, "CONNECT %s:%d HTTP/1.1\r\nHost: %s:%d\r\nProxy-Authorization: %s\r\n\r\n",
		host, port, host, port, basicAuth(credential))
}

func isConnect(req []byte) bool {
	method, _, _ := bytes.Cut(firstLine(req), []byte(" "))
	return bytes.EqualFold(method, []byte("CONNECT"))
}

// requestTarget returns the second field of the request line.
func requestTarget(req []byte) string {
	parts := bytes.SplitN(firstLine(req), []byte(" "), requestLineParts)
	if len(parts) < minRequestLineParts {
		return ""
	}
	return string(parts[1])
}

// logHost reduces a request target to host or host:port for logging. A path or
// query can carry a customer's token, so neither is logged.
func logHost(target string) string {
	if i := strings.Index(target, "://"); i >= 0 {
		target = target[i+schemeSepLen:]
	}
	if i := strings.IndexAny(target, "/?#"); i >= 0 {
		target = target[:i]
	}
	if i := strings.LastIndex(target, "@"); i >= 0 {
		target = target[i+1:]
	}
	if len(target) > maxLogHost {
		target = target[:maxLogHost]
	}
	return target
}
