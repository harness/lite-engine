// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"testing"
	"time"
)

const (
	getExample  = "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"
	connectLine = "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
)

// deadProxy is a proxy address that refuses connections.
func deadProxy(t *testing.T) *fakeProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()
	return &fakeProxy{addr: addr, reqs: make(chan []byte, 1)}
}

func closeWrite(t *testing.T, c net.Conn) {
	t.Helper()
	tc, ok := c.(*net.TCPConn)
	if !ok {
		t.Fatalf("%T is not a TCP connection", c)
	}
	if err := tc.CloseWrite(); err != nil {
		t.Fatal(err)
	}
}

func TestUnreachableProxyIsLoggedForEveryRelayKind(t *testing.T) {
	proxy := deadProxy(t)
	tests := []struct {
		name string
		port int
		send []byte
	}{
		{name: "tunnel", port: 443, send: buildClientHello(t, "pypi.org")},
		{name: "plain http", port: 80, send: []byte("GET /x HTTP/1.1\r\nHost: example.com\r\n\r\n")},
		{name: "explicit proxy client", port: proxy.port(t), send: []byte(getExample)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			logs := &lockedBuf{}
			srv := newTestServer(t, proxy, logs)
			runHandle(t, srv, "1.2.3.4", tt.port, func(c net.Conn) {
				_, _ = c.Write(tt.send)
				_ = readAll(c)
			})
			if !strings.Contains(logs.String(), "upstream dial:") {
				t.Fatalf("logs = %q", logs.String())
			}
			assertNoCredential(t, logs)
		})
	}
}

func TestProxyThatHangsUpWithoutAnsweringIsLogged(t *testing.T) {
	tests := []struct {
		name string
		port func(*fakeProxy) int
		send []byte
		want string
	}{
		{name: "tunnel", port: func(*fakeProxy) int { return 443 }, send: buildClientHello(t, "pypi.org"), want: "upstream rejected pypi.org:443: EOF"},
		{name: "explicit connect", port: func(p *fakeProxy) int { return p.port(t) }, send: []byte(connectLine), want: "upstream rejected example.com:443: EOF"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := startProxy(t, func(net.Conn, []byte) {})
			logs := &lockedBuf{}
			srv := newTestServer(t, proxy, logs)
			runHandle(t, srv, "1.2.3.4", tt.port(proxy), func(c net.Conn) {
				_, _ = c.Write(tt.send)
				_ = readAll(c)
			})
			if !strings.Contains(logs.String(), tt.want) {
				t.Fatalf("logs = %q, want %q", logs.String(), tt.want)
			}
			assertNoCredential(t, logs)
		})
	}
}

func TestProxyAnswerThatNeverEndsIsRefused(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\n"))
		junk := []byte(strings.Repeat("X-Junk: yes\r\n", 64))
		for sent := 0; sent <= maxResponseHead; sent += len(junk) {
			if _, err := c.Write(junk); err != nil {
				return
			}
		}
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello(t, "pypi.org"))
		if got := readAll(c); got != "" {
			t.Fatalf("the client received %d bytes of the proxy's junk", len(got))
		}
	})
	if !strings.Contains(logs.String(), "upstream rejected pypi.org:443: HTTP/1.1 200 OK") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestFirstRequestThatCannotBeRewrittenIsRefused(t *testing.T) {
	tests := []struct {
		name string
		port func(*fakeProxy) int
		send string
		half bool
	}{
		{name: "plain http without a host", port: func(*fakeProxy) int { return 80 }, send: "GET / HTTP/1.1\r\n\r\n"},
		{name: "explicit request cut off in its head", port: func(p *fakeProxy) int { return p.port(t) }, send: "CONNECT example.com:443 HTTP/1.1\r\nHost: ex", half: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
			logs := &lockedBuf{}
			srv := newTestServer(t, proxy, logs)
			runHandle(t, srv, "1.2.3.4", tt.port(proxy), func(c net.Conn) {
				_, _ = c.Write([]byte(tt.send))
				if tt.half {
					closeWrite(t, c)
				}
				_ = readAll(c)
			})
			if !strings.Contains(logs.String(), "sidecar error:") {
				t.Fatalf("logs = %q", logs.String())
			}
			select {
			case got := <-proxy.reqs:
				t.Fatalf("the proxy received %q", got)
			default:
			}
		})
	}
}

func TestSilentClientTimeoutIsLoggedAndPlainCloseIsNot(t *testing.T) {
	proxy := startProxy(t, func(net.Conn, []byte) {})

	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	srv.firstRead = 100 * time.Millisecond
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) { _ = readAll(c) })
	if !strings.Contains(logs.String(), "read:") {
		t.Fatalf("a client that sent nothing logged %q", logs.String())
	}

	logs = &lockedBuf{}
	srv = newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 443, func(net.Conn) {})
	if got := logs.String(); got != "" {
		t.Fatalf("a client that closed at once logged %q", got)
	}
}

// A refusal that carries no length is passed on as the head alone: the sidecar
// cannot know where its body ends, and the proxy may hold the connection open.
func TestRefusalWithoutLengthIsForwardedAsTheHeadOnly(t *testing.T) {
	const refusal = "HTTP/1.1 403 Forbidden\r\nConnection: close\r\n\r\n"
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(refusal)) })
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "172.16.0.11", proxy.port(t), func(c net.Conn) {
		_, _ = c.Write([]byte(connectLine))
		if got := readAll(c); got != refusal {
			t.Fatalf("client received %q", got)
		}
	})
	if !strings.Contains(logs.String(), "upstream rejected example.com:443: HTTP/1.1 403 Forbidden") {
		t.Fatalf("logs = %q", logs.String())
	}
}

func TestMalformedChunkedBodyEndsTheRequest(t *testing.T) {
	const head = "POST /up HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n"
	tests := []struct {
		name  string
		parts []string
	}{
		{name: "bad size in the first read", parts: []string{head + "zz\r\n"}},
		{name: "bad size in a later read", parts: []string{head + "3\r\nabc\r\n", "zz\r\n"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			proxy := startProxyRaw(t, func(c net.Conn, reqs chan<- []byte) {
				reqs <- nil
				_, _ = io.Copy(io.Discard, c)
			})
			logs := &lockedBuf{}
			srv := newTestServer(t, proxy, logs)
			runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
				for i, part := range tt.parts {
					if i > 0 {
						time.Sleep(50 * time.Millisecond)
					}
					_, _ = c.Write([]byte(part))
				}
				_ = readAll(c)
			})
			if !strings.Contains(logs.String(), errMalformedBody.Error()) {
				t.Fatalf("logs = %q", logs.String())
			}
		})
	}
}

func TestBodyShorterThanItsLengthEndsTheRequest(t *testing.T) {
	proxy := startProxy(t, func(net.Conn, []byte) {})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("POST /up HTTP/1.1\r\nHost: example.com\r\nContent-Length: 100\r\n\r\n0123456789"))
		closeWrite(t, c)
		_ = readAll(c)
	})
	if got := string(proxy.request(t)); !strings.HasPrefix(got, "POST http://example.com/up HTTP/1.1\r\n") {
		t.Fatalf("the proxy received %q", got)
	}
}

// pipeConns returns a client and upstream pair backed by in-memory pipes. The
// upstream's peer is closed, so every write to it fails at once. The client's
// peer is returned for the test to feed.
func pipeConns(t *testing.T) (client, upstream, clientPeer net.Conn) {
	t.Helper()
	client, clientPeer = net.Pipe()
	upstream, upstreamPeer := net.Pipe()
	_ = upstreamPeer.Close()
	t.Cleanup(func() {
		_ = client.Close()
		_ = clientPeer.Close()
		_ = upstream.Close()
	})
	return client, upstream, clientPeer
}

func TestForwardRequestReportsAFailedUpstreamWrite(t *testing.T) {
	tests := []struct {
		name    string
		request string
	}{
		{name: "request without a complete head", request: "GET / HTTP/1.1\r\nHost: example.com"},
		{name: "head", request: "GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"},
		{name: "chunked body in the first read", request: "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n3\r\nabc\r\n0\r\n\r\n"},
		{name: "fixed body in the first read", request: "POST / HTTP/1.1\r\nContent-Length: 3\r\n\r\nabc"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, upstream, _ := pipeConns(t)
			act := &xfer{idle: time.Second, conns: []net.Conn{client, upstream}}
			if err := forwardRequest(upstream, client, []byte(tt.request), act); err == nil {
				t.Fatal("forwardRequest succeeded against a closed upstream")
			}
		})
	}
}

func TestForwardRequestReportsAFailedUpstreamWriteOfALaterRead(t *testing.T) {
	tests := []struct {
		name    string
		request string
		more    string
	}{
		{name: "chunked body", request: "POST / HTTP/1.1\r\nTransfer-Encoding: chunked\r\n\r\n", more: "3\r\nabc\r\n0\r\n\r\n"},
		{name: "fixed body", request: "POST / HTTP/1.1\r\nContent-Length: 10\r\n\r\n", more: "0123456789"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, clientPeer := net.Pipe()
			t.Cleanup(func() { _ = client.Close(); _ = clientPeer.Close() })
			go func() { _, _ = clientPeer.Write([]byte(tt.more)) }()

			// The head goes through, so it is the body write that fails.
			upstream := &headOnlyConn{limit: len(tt.request)}
			act := &xfer{idle: time.Second, conns: []net.Conn{client}}
			if err := forwardRequest(upstream, client, []byte(tt.request), act); err == nil {
				t.Fatal("forwardRequest succeeded although the body write failed")
			}
		})
	}
}

// headOnlyConn accepts the first limit bytes written to it and fails every
// write after that.
type headOnlyConn struct {
	net.Conn
	limit   int
	written int
}

func (h *headOnlyConn) Write(p []byte) (int, error) {
	if h.written >= h.limit {
		return 0, errors.New("upstream closed")
	}
	h.written += len(p)
	return len(p), nil
}

func (h *headOnlyConn) SetWriteDeadline(time.Time) error { return nil }

func TestHTTPHelpersRefuseMalformedInput(t *testing.T) {
	for _, head := range []string{"Content-Length: abc", "Content-Length: -5", "Content-Length: "} {
		if n, ok := contentLength([]byte("POST / HTTP/1.1\r\n" + head + "\r\n\r\n")); ok {
			t.Fatalf("contentLength(%q) = %d, true", head, n)
		}
	}
	if got := statusCode([]byte("garbage")); got != "" {
		t.Fatalf("statusCode = %q, want empty", got)
	}
	if got := requestTarget([]byte("GET")); got != "" {
		t.Fatalf("requestTarget = %q, want empty", got)
	}
}

func TestReadFirstStopsAtItsLimit(t *testing.T) {
	client, peer := net.Pipe()
	t.Cleanup(func() { _ = client.Close(); _ = peer.Close() })
	go func() {
		_, _ = peer.Write([]byte("GET /" + strings.Repeat("a", 2*maxFirstRead)))
	}()
	got, err := readFirst(client, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) < maxFirstRead {
		t.Fatalf("readFirst returned %d bytes, want at least %d", len(got), maxFirstRead)
	}
}

func TestTLSRecordNeedsItsHeader(t *testing.T) {
	if tlsRecordComplete([]byte{tlsHandshakeRecord, 3}) {
		t.Fatal("a two-byte prefix was reported as a complete record")
	}
}

func newServerForServe(t *testing.T, listen string, logs *lockedBuf) *Server {
	t.Helper()
	srv, err := New(&Config{
		ListenAddr: listen,
		ProxyHost:  "127.0.0.1",
		ProxyPort:  3128,
		Credential: testLogin(),
		Log:        log.New(logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func TestListenAndServeStopsWhenClosed(t *testing.T) {
	logs := &lockedBuf{}
	srv := newServerForServe(t, "127.0.0.1:0", logs)
	done := make(chan error, 1)
	go func() { done <- srv.ListenAndServe() }()

	deadline := time.Now().Add(testDeadline)
	for !strings.Contains(logs.String(), "sidecar listening on") {
		if time.Now().After(deadline) {
			t.Fatalf("the server never started: %q", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testDeadline):
		t.Fatal("ListenAndServe did not return after Close")
	}
}

func TestListenAndServeReportsAnAddressInUse(t *testing.T) {
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	srv := newServerForServe(t, held.Addr().String(), &lockedBuf{})
	if err := srv.ListenAndServe(); err == nil {
		t.Fatal("ListenAndServe succeeded on an address that is in use")
	}
}

func TestCloseBeforeServeDoesNothing(t *testing.T) {
	srv := newServerForServe(t, "127.0.0.1:0", &lockedBuf{})
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
}

func TestConnectionWithoutAnOriginalDestinationIsDropped(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
	logs := &lockedBuf{}
	addr := newLoopbackServer(t, proxy, logs, func(net.Conn) (string, int, error) {
		return "", 0, errors.New("no original destination")
	})
	c, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(testDeadline))
	_, _ = c.Write([]byte(getExample))
	_ = readAll(c)
	if !strings.Contains(logs.String(), "original destination: no original destination") {
		t.Fatalf("logs = %q", logs.String())
	}
	select {
	case got := <-proxy.reqs:
		t.Fatalf("the proxy received %q", got)
	default:
	}
}
