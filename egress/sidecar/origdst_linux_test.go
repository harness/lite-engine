// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build linux

package sidecar

import (
	"errors"
	"net"
	"testing"
)

// The success path needs an iptables REDIRECT rule, so it runs only on a real
// VM. These cover the ways the lookup fails.

func TestOriginalDstRefusesAConnectionWithoutASocket(t *testing.T) {
	a, b := net.Pipe()
	t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
	if _, _, err := originalDst(a); !errors.Is(err, errNoSocket) {
		t.Fatalf("err = %v, want errNoSocket", err)
	}
}

func dialLoopback(t *testing.T) net.Conn {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	go func() {
		if c, err := ln.Accept(); err == nil {
			t.Cleanup(func() { _ = c.Close() })
		}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return c
}

// A connection the sidecar accepts that no REDIRECT rule touched is refused by
// serveConn when the lookup fails or reports the listener's own address. The
// lookup runs on the accepted (server-side) socket, as serveConn does. On the
// client side the kernel reports the peer instead, which is not the case
// serveConn guards against.
func TestOriginalDstOfAnAcceptedConnectionThatWasNotRedirectedIsRefused(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = ln.Close() })
	accepted := make(chan net.Conn, 1)
	go func() {
		if c, err := ln.Accept(); err == nil {
			accepted <- c
		}
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	server := <-accepted
	t.Cleanup(func() { _ = server.Close() })

	ip, port, err := originalDst(server)
	if err == nil && !isLocalAddr(server.LocalAddr(), ip, port) {
		t.Fatalf("originalDst = %s:%d on accepted local %v; serveConn would relay it", ip, port, server.LocalAddr())
	}
}

func TestOriginalDstFailsForAClosedConnection(t *testing.T) {
	c := dialLoopback(t)
	_ = c.Close()
	if _, _, err := originalDst(c); err == nil {
		t.Fatal("originalDst succeeded on a closed connection")
	}
}
