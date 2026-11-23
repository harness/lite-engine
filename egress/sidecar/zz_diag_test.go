//go:build linux

package sidecar

import (
	"net"
	"testing"
)

// TEMPORARY diagnostic: calls originalDst on the accepted (server-side) socket,
// the same side serveConn uses. Always fails so the values show in CI logs.
func TestDiagnosticAcceptedSideOriginalDst(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	type result struct {
		local, remote string
		ip            string
		port          int
		err           error
	}
	done := make(chan result, 1)
	go func() {
		c, err := ln.Accept()
		if err != nil {
			done <- result{err: err}
			return
		}
		defer c.Close()
		ip, port, err := originalDst(c)
		done <- result{local: c.LocalAddr().String(), remote: c.RemoteAddr().String(), ip: ip, port: port, err: err}
	}()
	c, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	r := <-done
	t.Fatalf("DIAG accepted side: local=%s remote=%s originalDst=%s:%d err=%v", r.local, r.remote, r.ip, r.port, r.err)
}
