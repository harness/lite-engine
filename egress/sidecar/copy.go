// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"bytes"
	"errors"
	"io"
	"net"
	"strings"
	"sync/atomic"
	"time"
)

// xfer is the idle timer shared by the connections of one relay. Every read
// pushes the read deadline of all of them out by idle, so a connection is idle
// only when no bytes move in either direction. A read that times out while a
// write is still in progress is a slow peer, not an idle one.
type xfer struct {
	idle    time.Duration
	conns   []net.Conn
	writing atomic.Int32
}

func (s *Server) newXfer(conns ...net.Conn) *xfer {
	return &xfer{idle: s.idle, conns: conns}
}

func (x *xfer) read(c net.Conn, p []byte) (int, error) {
	deadline := time.Now().Add(x.idle)
	for _, conn := range x.conns {
		_ = conn.SetReadDeadline(deadline)
	}
	return c.Read(p)
}

// write sends p with a write deadline, so a peer that stops reading cannot hold
// the relay open forever.
func (x *xfer) write(dst net.Conn, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	x.writing.Add(1)
	defer x.writing.Add(-1)
	_ = dst.SetWriteDeadline(time.Now().Add(x.idle))
	_, err := dst.Write(p)
	return err
}

// pipe copies src to dst until EOF or an error. EOF and a closed connection are
// not errors. observe, when set, sees every chunk before it is written.
func (x *xfer) pipe(dst, src net.Conn, observe func([]byte)) error {
	buf := make([]byte, copyBufferSize)
	for {
		n, err := x.read(src, buf)
		if n > 0 {
			if observe != nil {
				observe(buf[:n])
			}
			if werr := x.write(dst, buf[:n]); werr != nil {
				return werr
			}
		}
		switch {
		case err == nil:
		case errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
			return nil
		case isTimeout(err) && x.writing.Load() > 0:
		default:
			return err
		}
	}
}

// splice copies both directions until either side ends, then closes both.
func (s *session) splice(a, b net.Conn, act *xfer) {
	done := make(chan error, 1)
	go func() {
		var err error
		defer func() { done <- err }()
		defer s.recoverPanic()
		err = act.pipe(b, a, nil)
		closeBoth(a, b)
	}()
	err := act.pipe(a, b, nil)
	closeBoth(a, b)
	if err == nil {
		err = <-done
	} else {
		<-done
	}
	s.logIOError("tunnel", err)
}

func closeBoth(a, b net.Conn) {
	_ = a.Close()
	_ = b.Close()
}

// logIOError logs an error that ends a relay. EOF and a closed connection are
// the normal ends and are not logged.
func (s *session) logIOError(what string, err error) {
	switch {
	case err == nil, errors.Is(err, io.EOF), errors.Is(err, net.ErrClosed):
	case isTimeout(err):
		s.logf("idle timeout")
	default:
		s.logf("%s: %v", what, err)
	}
}

// writeTimeout writes p to conn with a deadline.
func writeTimeout(conn net.Conn, p []byte, d time.Duration) error {
	_ = conn.SetWriteDeadline(time.Now().Add(d))
	_, err := conn.Write(p)
	return err
}

// statusLogger logs the status line of a proxied response when it is an error.
// An interim 1xx response, such as 100 Continue, is skipped so the final status
// is the one logged.
type statusLogger struct {
	s       *session
	where   string
	pending []byte
	logged  bool
}

func (l *statusLogger) observe(p []byte) {
	for !l.logged {
		l.pending = append(l.pending, p...)
		p = nil
		line, _, found := bytes.Cut(l.pending, crlf)
		if !found {
			if len(l.pending) >= maxStatusLine {
				l.log(string(l.pending))
			}
			return
		}
		if !strings.HasPrefix(statusCode(line), "1") {
			l.log(string(line))
			return
		}
		_, after, headDone := bytes.Cut(l.pending, crlfcrlf)
		if !headDone {
			if len(l.pending) > maxResponseHead {
				l.stop()
			}
			return
		}
		l.pending = after
		if len(l.pending) == 0 {
			return
		}
	}
}

func (l *statusLogger) log(status string) {
	l.stop()
	l.s.logStatus(l.where, status)
}

// stop ends observation without logging, for an interim head too large to
// buffer.
func (l *statusLogger) stop() {
	l.logged = true
	l.pending = nil
}

// finish logs an empty response when nothing was ever seen.
func (l *statusLogger) finish() {
	if !l.logged {
		l.log("")
	}
}
