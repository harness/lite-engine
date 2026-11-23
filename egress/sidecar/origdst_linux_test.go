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

// A connection that no REDIRECT rule touched has no original destination.
func TestOriginalDstFailsForAConnectionThatWasNotRedirected(t *testing.T) {
	if ip, port, err := originalDst(dialLoopback(t)); err == nil {
		t.Fatalf("originalDst = %s:%d, nil; want an error", ip, port)
	}
}

func TestOriginalDstFailsForAClosedConnection(t *testing.T) {
	c := dialLoopback(t)
	_ = c.Close()
	if _, _, err := originalDst(c); err == nil {
		t.Fatal("originalDst succeeded on a closed connection")
	}
}
