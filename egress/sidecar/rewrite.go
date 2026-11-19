package sidecar

import (
	"bytes"
	"errors"
)

var (
	crlf     = []byte("\r\n")
	crlfcrlf = []byte("\r\n\r\n")

	// ErrIncompleteHead means the request head has no blank line yet.
	ErrIncompleteHead = errors.New("request head incomplete")
	// ErrMalformedRequestLine means the first line is not "METHOD target [version]".
	ErrMalformedRequestLine = errors.New("malformed request line")
	// ErrNoHost means a plain HTTP request on port 80 has no Host header, so an
	// absolute-URI cannot be built.
	ErrNoHost = errors.New("request has no Host header")
)

// RewriteExplicit prepares a request that already speaks proxy HTTP (port 3128)
// for the fleet proxy. CONNECT headers are kept as the client sent them, apart
// from the credential. A plain request gets Connection: close, because the
// sockets are spliced after the first response, and a kept-alive second request
// would otherwise reach the proxy with no credential and get 407.
//
// It returns the rewritten request and whether the client sent its own
// Proxy-Authorization. The credential itself is never returned in an error.
func RewriteExplicit(first []byte, credential string) ([]byte, bool, error) {
	head, rest, ok := bytes.Cut(first, crlfcrlf)
	if !ok {
		return nil, false, ErrIncompleteHead
	}
	lines := bytes.Split(head, crlf)
	method, _, _ := bytes.Cut(lines[0], []byte(" "))
	isConnect := bytes.EqualFold(method, []byte("CONNECT"))
	out, clientAuth := rewriteHead(lines, BasicAuth(credential), isConnect)
	return joinHead(out, rest), clientAuth, nil
}

// RewriteAbsolute prepares a plain HTTP request on port 80 as an absolute-URI
// proxy request, which is what the fleet proxy accepts on that port. CONNECT is
// not used: the fleet chart sends every CONNECT, including CONNECT host:80, to
// its TLS intercept listener.
func RewriteAbsolute(first []byte, credential string) ([]byte, bool, error) {
	head, rest, ok := bytes.Cut(first, crlfcrlf)
	if !ok {
		return nil, false, ErrIncompleteHead
	}
	lines := bytes.Split(head, crlf)
	parts := bytes.Split(lines[0], []byte(" "))
	if len(parts) < 2 {
		return nil, false, ErrMalformedRequestLine
	}
	method, target := parts[0], parts[1]
	version := []byte("HTTP/1.1")
	if len(parts) > 2 {
		version = parts[2]
	}
	host := hostHeader(lines[1:])
	if host == nil {
		return nil, false, ErrNoHost
	}
	absolute := target
	if !bytes.HasPrefix(target, []byte("http://")) && !bytes.HasPrefix(target, []byte("https://")) {
		path := target
		if !bytes.HasPrefix(path, []byte("/")) {
			path = append([]byte("/"), path...)
		}
		absolute = concat([]byte("http://"), host, path)
	}
	lines[0] = concat(method, []byte(" "), absolute, []byte(" "), version)
	out, clientAuth := rewriteHead(lines, BasicAuth(credential), false)
	return joinHead(out, rest), clientAuth, nil
}

// rewriteHead applies the credential and Connection handling shared by both
// ports. lines[0] is the request line. For CONNECT, the other headers are kept
// as sent. For a plain request, Connection and Proxy-Connection become
// Connection: close.
func rewriteHead(lines [][]byte, auth string, isConnect bool) ([][]byte, bool) {
	out := [][]byte{lines[0]}
	clientAuth := false
	sawConnection := false
	for _, line := range lines[1:] {
		lower := bytes.ToLower(line)
		if bytes.HasPrefix(lower, []byte("proxy-authorization:")) {
			clientAuth = true
		}
		if !isConnect && (bytes.HasPrefix(lower, []byte("connection:")) || bytes.HasPrefix(lower, []byte("proxy-connection:"))) {
			sawConnection = true
			out = append(out, []byte("Connection: close"))
			continue
		}
		out = append(out, line)
	}
	if !clientAuth {
		withAuth := make([][]byte, 0, len(out)+1)
		withAuth = append(withAuth, out[0], []byte("Proxy-Authorization: "+auth))
		withAuth = append(withAuth, out[1:]...)
		out = withAuth
	}
	if !isConnect && !sawConnection {
		out = append(out, []byte("Connection: close"))
	}
	return out, clientAuth
}

// hostHeader returns the value of the first Host header, or nil if there is none.
func hostHeader(headers [][]byte) []byte {
	for _, line := range headers {
		if bytes.HasPrefix(bytes.ToLower(line), []byte("host:")) {
			_, value, _ := bytes.Cut(line, []byte(":"))
			return bytes.TrimSpace(value)
		}
	}
	return nil
}

func joinHead(lines [][]byte, rest []byte) []byte {
	out := bytes.Join(lines, crlf)
	out = append(out, crlfcrlf...)
	return append(out, rest...)
}

func concat(parts ...[]byte) []byte {
	return bytes.Join(parts, nil)
}
