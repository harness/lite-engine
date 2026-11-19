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

// session is one relayed connection. Its log lines carry a connection number,
// so the lines of concurrent connections can be told apart, and a refusal
// names the credential source, so a wrong client credential is not mistaken
// for a wrong sidecar credential. A connection that succeeds logs nothing.
type session struct {
	*Server
	id     uint64
	source string
}

// handle relays one redirected connection. dstIP and dstPort are the address
// the client dialed before iptables redirected it.
func (s *Server) handle(client net.Conn, dstIP string, dstPort int) {
	c := &session{Server: s, id: s.connID.Add(1)}
	c.relay(client, dstIP, dstPort)
}

func (s *session) relay(client net.Conn, dstIP string, dstPort int) {
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
func (s *session) relayExplicit(client net.Conn, first []byte) {
	request, clientAuth, err := rewriteExplicit(first, s.credential)
	if err != nil {
		s.logf("sidecar error: %v", err)
		return
	}
	s.setSource(clientAuth)
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
	if err := writeTimeout(client, res.raw, s.idle); err != nil {
		return
	}
	if !res.ok {
		s.forwardRefusalBody(client, upstream, res)
		return
	}
	s.tunnel(client, upstream, rest, nil)
}

// forwardRefusalBody sends the rest of a refused CONNECT's body, such as a
// block page, when the proxy announced its length. The proxy may keep the
// connection open after the body, so it is not read until close.
func (s *session) forwardRefusalBody(client, upstream net.Conn, res connectResult) {
	n, ok := contentLength(res.raw[:res.headLen])
	if !ok {
		return
	}
	remaining := n - (len(res.raw) - res.headLen)
	if remaining <= 0 {
		return
	}
	act := &xfer{idle: s.headerTimeout, conns: []net.Conn{client, upstream}}
	s.logIOError("refusal body", copyN(client, upstream, remaining, act))
}

// relayAbsolute handles plain HTTP on port 80.
func (s *session) relayAbsolute(client net.Conn, first []byte) {
	request, clientAuth, err := rewriteAbsolute(first, s.credential)
	if err != nil {
		s.logf("sidecar error: %v", err)
		return
	}
	s.setSource(clientAuth)
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
func (s *session) relayTunnel(client net.Conn, first []byte, dstIP string, dstPort int) {
	host := ""
	if first[0] == tlsHandshakeRecord {
		host = sniFromClientHello(first)
	}
	if host == "" {
		host = dstIP
		s.logf("no SNI, using original destination %s:%d", dstIP, dstPort)
	}
	s.setSource(false)
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
	raw     []byte // everything read: the head, then any bytes the proxy sent after it
	headLen int
	ok      bool // 2xx
}

// connect sends a CONNECT head and reads the answer. A refusal is logged with
// the proxy's status line.
func (s *session) connect(upstream net.Conn, head []byte, where string) (connectResult, error) {
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
		s.logRejected(where, reason)
		return connectResult{}, err
	}
	res := connectResult{raw: raw, headLen: headLen, ok: strings.HasPrefix(statusCode(firstLine(raw)), "2")}
	if !res.ok {
		s.logRejected(where, string(firstLine(raw)))
	}
	return res, nil
}

// tunnel sends the client bytes already read, then the proxy bytes already
// read, and copies both directions until either side ends.
func (s *session) tunnel(client, upstream net.Conn, clientBytes, upstreamBytes []byte) {
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
func (s *session) exchange(client, upstream net.Conn, request []byte, where string) {
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

func (s *session) setSource(clientAuth bool) {
	s.source = "sidecar"
	if clientAuth {
		s.source = "client"
	}
}

func (s *session) logf(format string, args ...any) {
	s.Server.logf("conn=%d "+format, append([]any{s.id}, args...)...)
}

// recoverPanic is Server.recoverPanic with the connection number. It must be
// deferred directly.
func (s *session) recoverPanic() {
	if r := recover(); r != nil {
		s.logf("connection handler panic: %v", r)
	}
}

func (s *session) logRejected(where, status string) {
	s.logf("upstream rejected %s: %s (credential source=%s)", where, status, s.source)
}

func (s *session) logStatus(where, status string) {
	if status == "" {
		s.logf("upstream response %s: empty", where)
		return
	}
	// Only errors are logged. A redirect or 304 is a normal answer, and logging
	// it would add a line for most apt and package-manager requests.
	if code := statusCode([]byte(status)); !strings.HasPrefix(code, "4") && !strings.HasPrefix(code, "5") {
		return
	}
	s.logf("upstream response %s: %s (credential source=%s)", where, status, s.source)
}

func (s *Server) dial() (net.Conn, error) {
	d := net.Dialer{Timeout: s.dialTimeout}
	return d.Dial("tcp", net.JoinHostPort(s.proxyHost, strconv.Itoa(s.proxyPort)))
}
