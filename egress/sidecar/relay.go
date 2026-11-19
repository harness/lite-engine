// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"io"
	"net"
	"strconv"
	"strings"
	"time"
)

// handle relays one redirected connection. dstIP and dstPort are the address
// the client dialed before iptables redirected it.
func (s *Server) handle(client net.Conn, dstIP string, dstPort int) {
	first, err := readFirst(client, s.firstRead)
	if len(first) == 0 {
		if err != nil && !errors.Is(err, io.EOF) {
			s.logf("read: %v", err)
		}
		return
	}
	switch {
	case dstPort == s.proxyPort:
		s.relayExplicit(client, first)
	case dstPort == httpPort && first[0] != tlsHandshakeRecord:
		s.relayAbsolute(client, first)
	default:
		s.relayTunnel(client, first, dstIP, dstPort)
	}
}

// relayExplicit handles a client that already speaks proxy HTTP to the fleet
// proxy address. The credential is added only when the client sent none.
func (s *Server) relayExplicit(client net.Conn, first []byte) {
	request, clientAuth, err := rewriteExplicit(first, s.credential)
	if err != nil {
		s.logf("sidecar error: %v", err)
		return
	}
	s.logSource(clientAuth)
	upstream, err := s.dial()
	if err != nil {
		s.logf("upstream dial: %v", err)
		return
	}
	defer upstream.Close()
	where := logHost(requestTarget(request))
	if !isConnect(request) {
		s.exchange(client, upstream, request, where)
		return
	}
	head, rest, _ := splitHead(request)
	res, err := s.connect(upstream, head, where)
	if err != nil {
		return
	}
	// The client is waiting for the proxy's answer to its CONNECT, whether that
	// is a 200 or a refusal such as 403 or 407, so it gets the answer unchanged.
	if err := writeTimeout(client, res.raw, s.idle); err != nil || !res.ok {
		return
	}
	s.tunnel(client, upstream, rest, nil)
}

// relayAbsolute handles plain HTTP on port 80.
func (s *Server) relayAbsolute(client net.Conn, first []byte) {
	request, clientAuth, err := rewriteAbsolute(first, s.credential)
	if err != nil {
		s.logf("sidecar error: %v", err)
		return
	}
	s.logSource(clientAuth)
	upstream, err := s.dial()
	if err != nil {
		s.logf("upstream dial: %v", err)
		return
	}
	defer upstream.Close()
	s.exchange(client, upstream, request, logHost(requestTarget(request)))
}

// relayTunnel handles TLS, or any other bytes, on a redirected port. The
// client sent no CONNECT of its own, so the sidecar opens one to the host in
// the TLS server name, or to the original destination when there is none. The
// proxy's answer is consumed, because a client that sent raw TLS does not
// expect an HTTP response.
func (s *Server) relayTunnel(client net.Conn, first []byte, dstIP string, dstPort int) {
	host := ""
	if first[0] == tlsHandshakeRecord {
		host = sniFromClientHello(first)
	}
	if host == "" {
		host = dstIP
		s.logf("no SNI, using original destination %s:%d", dstIP, dstPort)
	}
	s.logSource(false)
	upstream, err := s.dial()
	if err != nil {
		s.logf("upstream dial: %v", err)
		return
	}
	defer upstream.Close()
	where := net.JoinHostPort(host, strconv.Itoa(dstPort))
	res, err := s.connect(upstream, connectHeader(host, dstPort, s.credential), where)
	if err != nil || !res.ok {
		return
	}
	s.tunnel(client, upstream, first, res.raw[res.headLen:])
}

// connectResult is the proxy's answer to a CONNECT.
type connectResult struct {
	raw     []byte // everything read: the head, then any tunnel bytes already sent
	headLen int
	ok      bool // 2xx
}

// connect sends a CONNECT head and reads the answer. A refusal is logged with
// the proxy's status line.
func (s *Server) connect(upstream net.Conn, head []byte, where string) (connectResult, error) {
	if err := writeTimeout(upstream, head, s.headerTimeout); err != nil {
		s.logf("upstream write: %v", err)
		return connectResult{}, err
	}
	raw, headLen, err := readResponseHead(upstream, s.headerTimeout)
	if err != nil {
		reason := string(firstLine(raw))
		if reason == "" {
			reason = err.Error()
		}
		s.logf("upstream rejected %s: %s", where, reason)
		return connectResult{}, err
	}
	res := connectResult{raw: raw, headLen: headLen, ok: strings.HasPrefix(statusCode(firstLine(raw)), "2")}
	if !res.ok {
		s.logf("upstream rejected %s: %s", where, firstLine(raw))
	}
	return res, nil
}

// tunnel sends the client bytes already read, then the proxy bytes already
// read, and copies both directions until either side ends.
func (s *Server) tunnel(client, upstream net.Conn, clientBytes, upstreamBytes []byte) {
	act := s.newXfer(client, upstream)
	if err := act.write(upstream, clientBytes); err != nil {
		s.logf("upstream write: %v", err)
		return
	}
	if err := act.write(client, upstreamBytes); err != nil {
		return
	}
	s.splice(client, upstream, act)
}

// exchange relays one plain HTTP request and its response at the same time, so
// a large request body is not held back for the response head. Only one request
// is relayed: a pipelined second request would reach the proxy with no
// credential and get 407.
//
// The upstream socket is not half-closed after the request. The fleet proxy is
// Envoy, which treats a read-side FIN as a client disconnect and can abort the
// response. Connection: close, set by the rewrite, makes the proxy close.
func (s *Server) exchange(client, upstream net.Conn, request []byte, where string) {
	act := s.newXfer(client, upstream)
	bodyDone := make(chan error, 1)
	go func() {
		var err error
		defer func() { bodyDone <- err }()
		defer s.recoverPanic()
		err = forwardRequest(upstream, client, request, act)
		if err != nil {
			_ = upstream.Close()
		}
	}()
	status := &statusLogger{s: s, where: where}
	respErr := act.pipe(client, upstream, status.observe)
	status.finish()
	_ = upstream.Close()
	s.logIOError("response", respErr)
	select {
	case bodyErr := <-bodyDone:
		s.logIOError("request body", bodyErr)
	case <-time.After(bodyDrainGrace):
		// The client has the whole response and is not sending more body. The
		// caller closes the client connection, which ends the goroutine's read.
	}
}

func (s *Server) logSource(clientAuth bool) {
	if clientAuth {
		s.logf("credential source=client")
		return
	}
	s.logf("credential source=sidecar")
}

func (s *Server) logStatus(where, status string) {
	if status == "" {
		s.logf("upstream response %s: empty", where)
		return
	}
	if strings.HasPrefix(statusCode([]byte(status)), "2") {
		return
	}
	s.logf("upstream response %s: %s", where, status)
}

func (s *Server) dial() (net.Conn, error) {
	d := net.Dialer{Timeout: s.dialTimeout}
	return d.Dial("tcp", net.JoinHostPort(s.proxyHost, strconv.Itoa(s.proxyPort)))
}
