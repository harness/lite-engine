// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"io"
	"log"
	"net"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testUser  = "acct|org"
	testToken = "fixture-value-not-a-real-secret"

	testDeadline = 5 * time.Second
)

func testLogin() string { return testUser + ":" + testToken }

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func portOf(t *testing.T, addr string) int {
	t.Helper()
	_, ps, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatal(err)
	}
	port, err := strconv.Atoi(ps)
	if err != nil {
		t.Fatal(err)
	}
	return port
}

// fakeProxy stands in for the fleet proxy. Every request it reads is sent on
// reqs, so a test never reads a capture before it exists.
type fakeProxy struct {
	addr string
	reqs chan []byte
}

func startProxyRaw(t *testing.T, serve func(c net.Conn, reqs chan<- []byte)) *fakeProxy {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	p := &fakeProxy{addr: ln.Addr().String(), reqs: make(chan []byte, 16)}
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(testDeadline))
				serve(c, p.reqs)
			}()
		}
	}()
	return p
}

func startProxy(t *testing.T, serve func(c net.Conn, req []byte)) *fakeProxy {
	t.Helper()
	return startProxyRaw(t, func(c net.Conn, reqs chan<- []byte) {
		req := readProxyRequest(c)
		reqs <- req
		serve(c, req)
	})
}

func (p *fakeProxy) request(t *testing.T) []byte {
	t.Helper()
	select {
	case r := <-p.reqs:
		return r
	case <-time.After(testDeadline):
		t.Fatal("the proxy did not receive a request")
		return nil
	}
}

func (p *fakeProxy) port(t *testing.T) int { return portOf(t, p.addr) }

func readProxyRequest(c net.Conn) []byte {
	var buf []byte
	tmp := make([]byte, readChunkSize)
	for !requestComplete(buf) {
		n, err := c.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			break
		}
	}
	return buf
}

func requestComplete(buf []byte) bool {
	head, body, ok := splitHead(buf)
	if !ok {
		return false
	}
	if isChunked(head) {
		var p chunkParser
		_, done := p.consume(body)
		return done
	}
	n, has := contentLength(head)
	return !has || len(body) >= n
}

func newTestServer(t *testing.T, proxy *fakeProxy, logs *lockedBuf) *Server {
	t.Helper()
	host, _, err := net.SplitHostPort(proxy.addr)
	if err != nil {
		t.Fatal(err)
	}
	srv, err := New(&Config{
		ProxyHost:     host,
		ProxyPort:     proxy.port(t),
		Credential:    testLogin(),
		IdleTimeout:   2 * time.Second,
		DialTimeout:   time.Second,
		FirstRead:     time.Second,
		HeaderTimeout: time.Second,
		Log:           log.New(logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

// startHandler accepts one loopback connection and hands it to srv.handle with
// the given original destination. done closes when handle returns.
func startHandler(t *testing.T, srv *Server, ip string, port int) (client net.Conn, done <-chan struct{}) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	finished := make(chan struct{})
	go func() {
		defer close(finished)
		c, err := ln.Accept()
		_ = ln.Close()
		if err != nil {
			return
		}
		defer c.Close()
		srv.handle(c, ip, port)
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = client.SetDeadline(time.Now().Add(testDeadline))
	t.Cleanup(func() { _ = client.Close() })
	return client, finished
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(testDeadline):
		t.Fatal("handler did not return")
	}
}

func runHandle(t *testing.T, srv *Server, ip string, port int, fn func(net.Conn)) {
	t.Helper()
	client, done := startHandler(t, srv, ip, port)
	fn(client)
	_ = client.Close()
	waitDone(t, done)
}

func readAll(conn net.Conn) string {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, conn)
	return buf.String()
}

// readHead reads from conn until the blank line that ends an HTTP head.
func readHead(t *testing.T, conn net.Conn) string {
	t.Helper()
	var buf []byte
	tmp := make([]byte, 1)
	for !bytes.HasSuffix(buf, crlfcrlf) {
		n, err := conn.Read(tmp)
		buf = append(buf, tmp[:n]...)
		if err != nil {
			t.Fatalf("read head: %v (got %q)", err, buf)
		}
	}
	return string(buf)
}

func assertNoCredential(t *testing.T, logs *lockedBuf) {
	t.Helper()
	got := logs.String()
	if strings.Contains(got, testToken) || strings.Contains(got, basicAuth(testLogin())) {
		t.Fatalf("log contains the credential:\n%s", got)
	}
}

func okResponse(body string) string {
	return "HTTP/1.1 200 OK\r\nContent-Length: " + strconv.Itoa(len(body)) + "\r\nConnection: close\r\n\r\n" + body
}
