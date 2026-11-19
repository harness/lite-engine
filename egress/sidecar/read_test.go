// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"net"
	"strings"
	"testing"
	"time"
)

func loopbackPair(t *testing.T) (client, server net.Conn) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	accepted := make(chan net.Conn, 1)
	go func() {
		c, _ := ln.Accept()
		accepted <- c
	}()
	client, err = net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	server = <-accepted
	t.Cleanup(func() { _ = client.Close(); _ = server.Close() })
	return client, server
}

// SSH on port 443 speaks first and has no blank line. It must not wait for the
// first-read timeout.
func TestReadFirstDoesNotStallOnNonHTTP(t *testing.T) {
	client, server := loopbackPair(t)
	_, _ = client.Write([]byte("SSH-2.0-OpenSSH_9.6\r\n"))
	start := time.Now()
	got, err := readFirst(server, 3*time.Second)
	if err != nil || !strings.HasPrefix(string(got), "SSH-2.0") {
		t.Fatalf("got %q, %v", got, err)
	}
	if time.Since(start) > time.Second {
		t.Fatalf("readFirst waited %s on a non-HTTP protocol", time.Since(start))
	}
}

func TestReadFirstWaitsForTheWholeHTTPHead(t *testing.T) {
	for _, method := range []string{"GET", "LOCK", "PROPFIND", "CONNECT"} {
		client, server := loopbackPair(t)
		go func() {
			_, _ = client.Write([]byte(method[:2]))
			time.Sleep(50 * time.Millisecond)
			_, _ = client.Write([]byte(method[2:] + " / HTTP/1.1\r\nHost: x\r\n"))
			time.Sleep(50 * time.Millisecond)
			_, _ = client.Write([]byte("\r\n"))
		}()
		got, err := readFirst(server, 3*time.Second)
		if err != nil || !strings.HasSuffix(string(got), "\r\n\r\n") || !strings.HasPrefix(string(got), method+" / ") {
			t.Fatalf("%s: got %q, %v", method, got, err)
		}
	}
}

func TestReadFirstReturnsPartialBytesOnTimeout(t *testing.T) {
	client, server := loopbackPair(t)
	_, _ = client.Write([]byte("GET / HTTP/1.1\r\nHost: x\r\n"))
	got, err := readFirst(server, 200*time.Millisecond)
	if err != nil || !strings.HasPrefix(string(got), "GET /") {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestReadFirstWaitsForTheWholeTLSRecord(t *testing.T) {
	client, server := loopbackPair(t)
	hello := buildClientHello(t, "pypi.org")
	go func() {
		_, _ = client.Write(hello[:10])
		time.Sleep(50 * time.Millisecond)
		_, _ = client.Write(hello[10:])
	}()
	got, err := readFirst(server, 3*time.Second)
	if err != nil || sniFromClientHello(got) != "pypi.org" {
		t.Fatalf("got %d bytes, %v", len(got), err)
	}
}
