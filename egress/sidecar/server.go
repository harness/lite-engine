// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

const acceptRetryDelay = 200 * time.Millisecond

// Server is the egress sidecar. It listens for redirected connections and
// speaks credentialed HTTP to the fleet proxy.
type Server struct {
	listenAddr    string
	proxyHost     string
	proxyPort     int
	credential    string
	idle          time.Duration
	dialTimeout   time.Duration
	firstRead     time.Duration
	headerTimeout time.Duration
	log           *log.Logger

	mu sync.Mutex
	ln net.Listener
}

// New refuses a missing proxy host or an incomplete credential. The error does
// not contain the credential.
func New(cfg *Config) (*Server, error) {
	c := *cfg
	c.applyDefaults()
	if c.ProxyHost == "" {
		return nil, errProxyHostRequired
	}
	credential, err := ParseCredential(c.Credential)
	if err != nil {
		return nil, err
	}
	return &Server{
		listenAddr:    c.ListenAddr,
		proxyHost:     c.ProxyHost,
		proxyPort:     c.ProxyPort,
		credential:    credential,
		idle:          c.IdleTimeout,
		dialTimeout:   c.DialTimeout,
		firstRead:     c.FirstRead,
		headerTimeout: c.HeaderTimeout,
		log:           c.Log,
	}, nil
}

// ListenAndServe accepts redirected connections until Close.
func (s *Server) ListenAndServe() error {
	ln, err := net.Listen("tcp", s.listenAddr)
	if err != nil {
		return err
	}
	return s.Serve(ln)
}

// Serve accepts on ln. An accept error is logged and retried. Closing the
// listener makes Serve return.
func (s *Server) Serve(ln net.Listener) error {
	s.mu.Lock()
	s.ln = ln
	s.mu.Unlock()
	s.logf("sidecar listening on %s -> %s:%d", ln.Addr(), s.proxyHost, s.proxyPort)
	for {
		conn, err := ln.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			s.logf("accept failed: %v", err)
			time.Sleep(acceptRetryDelay)
			continue
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	defer s.recoverPanic()
	ip, port, err := lookupOriginalDst(conn)
	if err != nil {
		s.logf("original destination: %v", err)
		return
	}
	s.handle(conn, ip, port)
}

// recoverPanic keeps a bug in the handling of one connection from taking the
// sidecar down for the whole VM. It must be deferred directly.
func (s *Server) recoverPanic() {
	if r := recover(); r != nil {
		s.logf("connection handler panic: %v", r)
	}
}

// Close stops the listener. In-flight connections are not canceled.
func (s *Server) Close() error {
	s.mu.Lock()
	ln := s.ln
	s.mu.Unlock()
	if ln == nil {
		return nil
	}
	return ln.Close()
}

func (s *Server) logf(format string, args ...any) {
	s.log.Printf(format, args...)
}

// lookupOriginalDst reads the pre-redirect destination of a connection. Tests
// replace it, because they have no iptables redirect.
var lookupOriginalDst = originalDst
