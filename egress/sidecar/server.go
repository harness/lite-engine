package sidecar

import (
	"errors"
	"log"
	"net"
	"sync"
	"time"
)

// Server is the egress sidecar. It listens for redirected connections and
// speaks credentialed HTTP to the fleet proxy. Nothing in lite-engine starts
// it; CI-26163 writes the credential, installs the redirect, and starts the
// process. Until then a build is unchanged.
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
func New(cfg Config) (*Server, error) {
	cfg = cfg.withDefaults()
	if cfg.ProxyHost == "" {
		return nil, errProxyHostRequired
	}
	credential, err := ParseCredential(cfg.Credential)
	if err != nil {
		return nil, err
	}
	return &Server{
		listenAddr:    cfg.ListenAddr,
		proxyHost:     cfg.ProxyHost,
		proxyPort:     cfg.ProxyPort,
		credential:    credential,
		idle:          cfg.IdleTimeout,
		dialTimeout:   cfg.DialTimeout,
		firstRead:     cfg.FirstRead,
		headerTimeout: cfg.HeaderTimeout,
		log:           cfg.Log,
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

// Serve accepts on ln. An accept error is logged and retried. Closing ln
// makes Serve return.
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
			time.Sleep(200 * time.Millisecond)
			continue
		}
		go s.serveConn(conn)
	}
}

func (s *Server) serveConn(conn net.Conn) {
	defer conn.Close()
	ip, port, err := lookupOriginalDst(conn)
	if err != nil {
		s.logf("original destination: %v", err)
		return
	}
	s.handle(conn, ip, port)
}

// Close stops the listener. In-flight connections are not cancelled.
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

// lookupOriginalDst is replaced by tests that do not have an iptables redirect.
var lookupOriginalDst = OriginalDst
