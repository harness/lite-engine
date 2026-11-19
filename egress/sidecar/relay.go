package sidecar

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync/atomic"
	"time"
)

// xfer counts a write that is in progress. A read that times out while a write
// is blocked is not an idle connection: the peer is slow, and the bytes are
// still moving. The ci-play pause measurement depends on this.
type xfer struct {
	writing atomic.Int32
	bump    func()
}

func (x *xfer) write(dst net.Conn, p []byte) error {
	if len(p) == 0 {
		return nil
	}
	x.writing.Add(1)
	defer x.writing.Add(-1)
	_, err := dst.Write(p)
	return err
}

func (s *Server) handle(client net.Conn, dstIP string, dstPort int) {
	first, err := ReadFirst(client, s.firstRead)
	if len(first) == 0 {
		if err != nil && !errors.Is(err, io.EOF) {
			s.logf("read: %v", err)
		}
		return
	}
	switch {
	case dstPort == s.proxyPort:
		s.relayExplicit(client, first)
	case dstPort == 80 && first[0] != 22:
		s.relayAbsolute(client, first)
	default:
		s.relayTunnel(client, first, dstIP, dstPort)
	}
}

func (s *Server) relayExplicit(client net.Conn, first []byte) {
	rewritten, clientAuth, err := RewriteExplicit(first, s.credential)
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
	if connectRequest(rewritten) {
		s.finishConnect(client, upstream, rewritten, requestTarget(rewritten))
		return
	}
	s.exchange(client, upstream, rewritten, requestTarget(rewritten))
}

func (s *Server) relayAbsolute(client net.Conn, first []byte) {
	rewritten, clientAuth, err := RewriteAbsolute(first, s.credential)
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
	s.exchange(client, upstream, rewritten, requestTarget(rewritten))
}

func (s *Server) relayTunnel(client net.Conn, first []byte, dstIP string, dstPort int) {
	host := ""
	if first[0] == 22 {
		host = SNIFromClientHello(first)
	}
	port := dstPort
	if host == "" {
		host = dstIP
		s.logf("no SNI, using original destination %s:%d", dstIP, dstPort)
	}
	host, _, _ = strings.Cut(host, ":")
	s.logSource(false)
	upstream, err := s.dial()
	if err != nil {
		s.logf("upstream dial: %v", err)
		return
	}
	defer upstream.Close()
	req := connectHeader(host, port, s.credential)
	s.finishConnect(client, upstream, append(req, first...), net.JoinHostPort(host, strconv.Itoa(port)))
}

// finishConnect sends a CONNECT, and only forwards the already-read client
// bytes once the proxy accepts the tunnel. A refused CONNECT is logged and the
// client is closed. The client sent raw TLS, so there is no HTTP response to
// hand back.
func (s *Server) finishConnect(client, upstream net.Conn, payload []byte, where string) {
	head, rest, _ := bytes.Cut(payload, crlfcrlf)
	if err := writeAll(upstream, append(append([]byte{}, head...), crlfcrlf...)); err != nil {
		s.logf("upstream write: %v", err)
		return
	}
	status, pending, err := readResponseHead(upstream, s.headerTimeout)
	if err != nil && status == "" {
		s.logf("upstream rejected %s: %v", where, err)
		return
	}
	if !bytes.Contains([]byte(status), []byte(" 200 ")) {
		shown := status
		if shown == "" {
			shown = "empty"
		}
		s.logf("upstream rejected %s: %s", where, shown)
		return
	}
	act := s.newXfer(client, upstream)
	if len(rest) > 0 {
		if err := act.write(upstream, rest); err != nil {
			s.logf("upstream write: %v", err)
			return
		}
	}
	if len(pending) > 0 {
		if err := act.write(client, pending); err != nil {
			return
		}
	}
	s.splice(client, upstream, act)
}

// exchange forwards one plain HTTP request and its response at the same time.
// The request body is not held back for the response head. Bytes after the
// first request are not forwarded: a pipelined second request would reach the
// proxy with no credential and get 407.
func (s *Server) exchange(client, upstream net.Conn, rewritten []byte, where string) {
	act := s.newXfer(client, upstream)
	errc := make(chan error, 1)
	go func() {
		err := writeOneRequest(upstream, client, rewritten, act)
		if err != nil {
			upstream.Close()
		} else {
			closeWrite(upstream)
		}
		errc <- err
	}()
	err := s.copyResponse(client, upstream, where, act)
	upstream.Close()
	bodyErr := <-errc
	if err != nil && !isTimeout(err) && !errors.Is(err, io.EOF) && !errors.Is(err, net.ErrClosed) {
		s.logf("response: %v", err)
	}
	if isTimeout(err) {
		s.logf("idle timeout")
	}
	if bodyErr != nil && !isTimeout(bodyErr) && !errors.Is(bodyErr, io.EOF) && !errors.Is(bodyErr, net.ErrClosed) {
		s.logf("request body: %v", bodyErr)
	}
}

func (s *Server) splice(a, b net.Conn, act *xfer) {
	done := make(chan struct{})
	go func() {
		err := s.copyAll(b, a, act)
		if isTimeout(err) {
			s.logf("idle timeout")
		}
		a.Close()
		b.Close()
		close(done)
	}()
	err := s.copyAll(a, b, act)
	if isTimeout(err) {
		s.logf("idle timeout")
	}
	a.Close()
	b.Close()
	<-done
}

func writeOneRequest(upstream net.Conn, client net.Conn, req []byte, act *xfer) error {
	head, body, ok := bytes.Cut(req, crlfcrlf)
	if !ok {
		return act.write(upstream, req)
	}
	if err := act.write(upstream, append(append([]byte{}, head...), crlfcrlf...)); err != nil {
		return err
	}
	if chunked(head) {
		parser := chunkParser{}
		n, done := parser.consume(body)
		if n > 0 {
			if err := act.write(upstream, body[:n]); err != nil {
				return err
			}
		}
		buf := make([]byte, 32*1024)
		for !done {
			act.bump()
			nr, err := client.Read(buf)
			if nr > 0 {
				n, done = parser.consume(buf[:nr])
				if n > 0 {
					if werr := act.write(upstream, buf[:n]); werr != nil {
						return werr
					}
				}
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	n, has := contentLength(head)
	if !has {
		return nil
	}
	have := len(body)
	if have > n {
		have = n
	}
	if have > 0 {
		if err := act.write(upstream, body[:have]); err != nil {
			return err
		}
	}
	if have < n {
		return copyExact(upstream, client, n-have, act)
	}
	return nil
}

func copyExact(dst net.Conn, src net.Conn, n int, act *xfer) error {
	buf := make([]byte, 32*1024)
	left := n
	for left > 0 {
		act.bump()
		want := len(buf)
		if want > left {
			want = left
		}
		nr, err := src.Read(buf[:want])
		if nr > 0 {
			left -= nr
			if werr := act.write(dst, buf[:nr]); werr != nil {
				return werr
			}
		}
		if err != nil {
			return err
		}
	}
	return nil
}

func (s *Server) copyAll(dst, src net.Conn, act *xfer) error {
	buf := make([]byte, 32*1024)
	for {
		act.bump()
		n, err := src.Read(buf)
		if n > 0 {
			if werr := act.write(dst, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			if isTimeout(err) && act.writing.Load() > 0 {
				continue
			}
			return err
		}
	}
}

func (s *Server) copyResponse(dst, src net.Conn, where string, act *xfer) error {
	buf := make([]byte, 32*1024)
	var pending []byte
	logged := false
	note := func(p []byte) {
		if logged {
			return
		}
		pending = append(pending, p...)
		line, _, found := bytes.Cut(pending, crlf)
		if !found && len(pending) < 512 {
			return
		}
		if !found {
			line = pending
		}
		s.logStatus(where, string(line))
		logged = true
		pending = nil
	}
	for {
		act.bump()
		n, err := src.Read(buf)
		if n > 0 {
			note(buf[:n])
			if werr := act.write(dst, buf[:n]); werr != nil {
				return werr
			}
		}
		if err != nil {
			if isTimeout(err) && act.writing.Load() > 0 {
				continue
			}
			if !logged {
				s.logStatus(where, "")
			}
			if errors.Is(err, io.EOF) || errors.Is(err, net.ErrClosed) {
				return nil
			}
			return err
		}
	}
}

func (s *Server) newXfer(conns ...net.Conn) *xfer {
	x := &xfer{}
	x.bump = func() {
		deadline := time.Now().Add(s.idle)
		for _, c := range conns {
			_ = c.SetReadDeadline(deadline)
		}
	}
	return x
}

func (s *Server) logSource(clientAuth bool) {
	if clientAuth {
		s.logf("credential source=client")
		return
	}
	s.logf("credential source=file")
}

func (s *Server) logStatus(where, status string) {
	if status == "" {
		s.logf("upstream response %s: empty", where)
		return
	}
	parts := strings.SplitN(status, " ", 3)
	if len(parts) >= 2 && strings.HasPrefix(parts[1], "2") {
		return
	}
	s.logf("upstream response %s: %s", where, status)
}

func (s *Server) dial() (net.Conn, error) {
	d := net.Dialer{Timeout: s.dialTimeout}
	return d.Dial("tcp", net.JoinHostPort(s.proxyHost, strconv.Itoa(s.proxyPort)))
}

func connectHeader(host string, port int, credential string) []byte {
	return fmt.Appendf(nil, "CONNECT %s:%d HTTP/1.1\r\nHost: %s:%d\r\nProxy-Authorization: %s\r\n\r\n",
		host, port, host, port, BasicAuth(credential))
}

func connectRequest(req []byte) bool {
	line, _, _ := bytes.Cut(req, crlf)
	method, _, _ := bytes.Cut(line, []byte(" "))
	return bytes.EqualFold(method, []byte("CONNECT"))
}

func requestTarget(req []byte) string {
	line, _, _ := bytes.Cut(req, crlf)
	parts := bytes.SplitN(line, []byte(" "), 3)
	if len(parts) < 2 {
		return ""
	}
	target := string(parts[1])
	if len(target) > 80 {
		return target[:80]
	}
	return target
}

func readResponseHead(conn net.Conn, timeout time.Duration) (string, []byte, error) {
	if timeout > 0 {
		_ = conn.SetReadDeadline(time.Now().Add(timeout))
		defer conn.SetReadDeadline(time.Time{})
	}
	buf := make([]byte, 0, 512)
	tmp := make([]byte, 4096)
	for !bytes.Contains(buf, crlfcrlf) && len(buf) < 65536 {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			break
		}
	}
	head, pending, found := bytes.Cut(buf, crlfcrlf)
	if !found {
		line, _, _ := bytes.Cut(buf, crlf)
		if len(line) == 0 {
			return "", nil, io.ErrUnexpectedEOF
		}
		return string(line), nil, io.ErrUnexpectedEOF
	}
	line, _, _ := bytes.Cut(head, crlf)
	return string(line), pending, nil
}

func writeAll(conn net.Conn, p []byte) error {
	_, err := conn.Write(p)
	return err
}

func closeWrite(conn net.Conn) {
	if cw, ok := conn.(interface{ CloseWrite() error }); ok {
		_ = cw.CloseWrite()
	}
}

func isTimeout(err error) bool {
	var ne net.Error
	return errors.As(err, &ne) && ne.Timeout()
}
