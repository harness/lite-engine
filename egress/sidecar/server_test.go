package sidecar

import (
	"bytes"
	"errors"
	"io"
	"log"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

const (
	testUser   = "acct|org"
	testSecret = "secret-token"
)

func testCredential() string { return testUser + ":" + testSecret }

type lockedBuf struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *lockedBuf) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *lockedBuf) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

func newTestServer(t *testing.T, proxy string, logs *lockedBuf) *Server {
	t.Helper()
	host, portStr, err := net.SplitHostPort(proxy)
	if err != nil {
		t.Fatal(err)
	}
	var port int
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	srv, err := New(Config{
		ProxyHost:     host,
		ProxyPort:     port,
		Credential:    testCredential(),
		IdleTimeout:   2 * time.Second,
		DialTimeout:   time.Second,
		FirstRead:     time.Second,
		HeaderTimeout: time.Second,
		Log:           log.New(logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	return srv
}

func serveProxy(t *testing.T, fn func(net.Conn)) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go func() {
				defer c.Close()
				_ = c.SetDeadline(time.Now().Add(3 * time.Second))
				fn(c)
			}()
		}
	}()
	return ln.Addr().String()
}

func runHandle(t *testing.T, srv *Server, ip string, port int, fn func(net.Conn)) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c, err := ln.Accept()
		if err != nil {
			return
		}
		defer c.Close()
		srv.handle(c, ip, port)
	}()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(4 * time.Second))
	fn(client)
	_ = client.Close()
	_ = ln.Close()
	select {
	case <-done:
	case <-time.After(4 * time.Second):
		t.Fatal("handler did not return")
	}
}

func readAllDeadline(conn net.Conn) string {
	var buf bytes.Buffer
	_, _ = io.Copy(&buf, conn)
	return buf.String()
}

func readProxyRequest(conn net.Conn) []byte {
	var buf []byte
	tmp := make([]byte, 4096)
	for {
		n, err := conn.Read(tmp)
		if n > 0 {
			buf = append(buf, tmp[:n]...)
		}
		if err != nil {
			return buf
		}
		if bytes.Contains(buf, []byte("\r\n\r\n")) && requestComplete(buf) {
			return buf
		}
	}
}

func requestComplete(buf []byte) bool {
	head, body, ok := bytes.Cut(buf, []byte("\r\n\r\n"))
	if !ok {
		return false
	}
	if chunked(head) {
		_, done := (&chunkParser{}).consume(body)
		return done
	}
	n, ok := contentLength(head)
	if !ok {
		return true
	}
	return len(body) >= n
}

func assertNoCredential(t *testing.T, logs *lockedBuf) {
	t.Helper()
	got := logs.String()
	if strings.Contains(got, testSecret) || strings.Contains(got, BasicAuth(testCredential())) {
		t.Fatalf("log contains the credential:\n%s", got)
	}
}

func TestTunnelUsesSNIOn443(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		_, _ = io.Copy(io.Discard, c)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	hello := buildClientHello("pypi.org")
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		if _, err := c.Write(hello); err != nil {
			t.Fatal(err)
		}
		_ = readAllDeadline(c)
	})
	if !bytes.HasPrefix(got, []byte("CONNECT pypi.org:443 HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if !bytes.Contains(got, []byte("Proxy-Authorization: "+BasicAuth(testCredential()))) {
		t.Fatalf("missing credential: %q", got)
	}
	if !strings.Contains(logs.String(), "credential source=file") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestTunnelWithoutSNIUsesOriginalDestination(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		if _, err := c.Write(buildClientHello("")); err != nil {
			t.Fatal(err)
		}
	})
	if !bytes.HasPrefix(got, []byte("CONNECT 1.2.3.4:443 HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if !strings.Contains(logs.String(), "no SNI, using original destination 1.2.3.4:443") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestTLSOnPort80KeepsOriginalPort(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		if _, err := c.Write(buildClientHello("example.com")); err != nil {
			t.Fatal(err)
		}
	})
	if !bytes.HasPrefix(got, []byte("CONNECT example.com:80 HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if bytes.Contains(got, []byte(":443")) {
		t.Fatalf("port 80 TLS was sent to 443: %q", got)
	}
	assertNoCredential(t, logs)
}

func TestPlainHTTP80IsAbsoluteURI(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 2\r\nConnection: close\r\n\r\nok"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET /releases/x.img HTTP/1.1\r\nHost: cloud-images.ubuntu.com\r\n\r\n"))
		if got := readAllDeadline(c); !strings.Contains(got, "200 OK") || !strings.HasSuffix(got, "ok") {
			t.Fatalf("response = %q", got)
		}
	})
	want := "GET http://cloud-images.ubuntu.com/releases/x.img HTTP/1.1\r\n"
	if !bytes.HasPrefix(got, []byte(want)) {
		t.Fatalf("request = %q", got)
	}
	if !bytes.Contains(got, []byte("Connection: close")) {
		t.Fatalf("missing close: %q", got)
	}
	assertNoCredential(t, logs)
}

func TestExplicitProxyKeepsClientCredential(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	_, portStr, _ := net.SplitHostPort(addr)
	port := 0
	for _, c := range portStr {
		port = port*10 + int(c-'0')
	}
	runHandle(t, srv, "172.16.0.11", port, func(c net.Conn) {
		_, _ = c.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic Y2xpZW50\r\n\r\n"))
	})
	if strings.Count(string(got), "Proxy-Authorization:") != 1 {
		t.Fatalf("auth count in %q", got)
	}
	if !bytes.Contains(got, []byte("Proxy-Authorization: Basic Y2xpZW50")) {
		t.Fatalf("client credential replaced: %q", got)
	}
	if !strings.Contains(logs.String(), "credential source=client") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestExplicitProxyAddsCredentialWhenMissing(t *testing.T) {
	var got []byte
	addr := serveProxy(t, func(c net.Conn) {
		got = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	_, portStr, _ := net.SplitHostPort(addr)
	port := atoi(portStr)
	runHandle(t, srv, "172.16.0.11", port, func(c net.Conn) {
		_, _ = c.Write([]byte("GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		_ = readAllDeadline(c)
	})
	if !bytes.Contains(got, []byte("Proxy-Authorization: "+BasicAuth(testCredential()))) {
		t.Fatalf("credential not added: %q", got)
	}
	if !strings.Contains(logs.String(), "credential source=file") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestPipelinedSecondRequestIsNotForwarded(t *testing.T) {
	for _, tc := range []struct {
		name string
		port func(proxy string) int
		req  string
	}{
		{
			name: "port 80",
			port: func(string) int { return 80 },
			req:  "GET /first HTTP/1.1\r\nHost: example.com\r\n\r\nGET /second HTTP/1.1\r\nHost: example.com\r\n\r\n",
		},
		{
			name: "port 3128",
			port: func(proxy string) int {
				_, ps, _ := net.SplitHostPort(proxy)
				return atoi(ps)
			},
			req: "GET http://example.com/first HTTP/1.1\r\nHost: example.com\r\n\r\nGET http://example.com/second HTTP/1.1\r\nHost: example.com\r\n\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var got []byte
			addr := serveProxy(t, func(c net.Conn) {
				got = readProxyRequest(c)
				if bytes.Contains(got, []byte("/second")) {
					_, _ = c.Write([]byte("HTTP/1.1 407 Proxy Authentication Required\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
					return
				}
				_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 4\r\nConnection: close\r\n\r\none\n"))
			})
			logs := &lockedBuf{}
			srv := newTestServer(t, addr, logs)
			var response string
			runHandle(t, srv, "1.2.3.4", tc.port(addr), func(c net.Conn) {
				if _, err := c.Write([]byte(tc.req)); err != nil {
					t.Fatal(err)
				}
				response = readAllDeadline(c)
			})
			if bytes.Contains(got, []byte("/second")) {
				t.Fatalf("second request was forwarded: %q", got)
			}
			if strings.Contains(response, "407") || !strings.Contains(response, "200 OK") {
				t.Fatalf("response = %q", response)
			}
			assertNoCredential(t, logs)
		})
	}
}

func TestLargePostDoesNotWaitForResponse(t *testing.T) {
	for _, tc := range []struct {
		name string
		port func(string) int
		head string
	}{
		{name: "port 80", port: func(string) int { return 80 }, head: "POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 262144\r\n\r\n"},
		{name: "port 3128", port: func(proxy string) int {
			_, ps, _ := net.SplitHostPort(proxy)
			return atoi(ps)
		}, head: "POST http://example.com/upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 262144\r\n\r\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sawHead := make(chan struct{})
			addr := serveProxy(t, func(c net.Conn) {
				var buf []byte
				tmp := make([]byte, 4096)
				for !bytes.Contains(buf, []byte("\r\n\r\n")) {
					n, err := c.Read(tmp)
					buf = append(buf, tmp[:n]...)
					if err != nil {
						return
					}
				}
				close(sawHead)
				n, _ := contentLength(bytes.Split(buf, []byte("\r\n\r\n"))[0])
				have := len(buf) - (bytes.Index(buf, []byte("\r\n\r\n")) + 4)
				left := n - have
				if left > 0 {
					if _, err := io.CopyN(io.Discard, c, int64(left)); err != nil {
						return
					}
				}
				_, _ = c.Write([]byte("HTTP/1.1 201 Created\r\nContent-Length: 4\r\nConnection: close\r\n\r\nsent"))
			})
			logs := &lockedBuf{}
			srv := newTestServer(t, addr, logs)
			body := bytes.Repeat([]byte("x"), 262144)
			var response string
			runHandle(t, srv, "1.2.3.4", tc.port(addr), func(c net.Conn) {
				if _, err := c.Write([]byte(tc.head)); err != nil {
					t.Fatal(err)
				}
				select {
				case <-sawHead:
				case <-time.After(3 * time.Second):
					t.Fatal("proxy did not see the request head")
				}
				if _, err := c.Write(body); err != nil {
					t.Fatal(err)
				}
				response = readAllDeadline(c)
			})
			if !strings.Contains(response, "201 Created") || !strings.HasSuffix(response, "sent") {
				t.Fatalf("response = %q", response)
			}
			assertNoCredential(t, logs)
		})
	}
}

func TestWrongTokenStatusIsPassedThrough(t *testing.T) {
	addr := serveProxy(t, func(c net.Conn) {
		_ = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 4\r\nConnection: close\r\n\r\nnope"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	var response string
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		response = readAllDeadline(c)
	})
	if !strings.Contains(response, "500") || !strings.Contains(response, "nope") {
		t.Fatalf("response = %q", response)
	}
	if !strings.Contains(logs.String(), "500") || !strings.Contains(logs.String(), "credential source=file") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestRejectedConnectClosesClient(t *testing.T) {
	addr := serveProxy(t, func(c net.Conn) {
		_ = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	runHandle(t, srv, "9.9.9.9", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello("blocked.example"))
		buf := make([]byte, 32)
		n, err := c.Read(buf)
		if n != 0 || (err != nil && !errors.Is(err, io.EOF)) {
			// A closed tunnel is the signal. Some stacks return EOF with no bytes.
			if n != 0 {
				t.Fatalf("client received %q, err %v", buf[:n], err)
			}
		}
	})
	if !strings.Contains(logs.String(), "upstream rejected blocked.example:443") || !strings.Contains(logs.String(), "403") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestIdleTimeoutClosesQuietTunnel(t *testing.T) {
	addr := serveProxy(t, func(c net.Conn) {
		_ = readProxyRequest(c)
		_, _ = c.Write([]byte("HTTP/1.1 200 Connection Established\r\n\r\n"))
		_, _ = io.Copy(io.Discard, c)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, addr, logs)
	srv.idle = 200 * time.Millisecond
	start := time.Now()
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello("pypi.org"))
		_ = readAllDeadline(c)
	})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("quiet tunnel stayed open for %s", time.Since(start))
	}
	if !strings.Contains(logs.String(), "idle timeout") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestAcceptRetries(t *testing.T) {
	logs := &lockedBuf{}
	srv, err := New(Config{
		ProxyHost:  "127.0.0.1",
		ProxyPort:  3128,
		Credential: testCredential(),
		Log:        log.New(logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	flaky := &failOnce{Listener: ln}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(flaky) }()
	client, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	_ = client.Close()
	time.Sleep(50 * time.Millisecond)
	if err := srv.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("Serve did not return")
	}
	if !strings.Contains(logs.String(), "accept failed") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

type failOnce struct {
	net.Listener
	once sync.Once
}

func (f *failOnce) Accept() (net.Conn, error) {
	failed := false
	f.once.Do(func() { failed = true })
	if failed {
		return nil, errors.New("temporary")
	}
	return f.Listener.Accept()
}

func atoi(s string) int {
	n := 0
	for _, c := range s {
		n = n*10 + int(c-'0')
	}
	return n
}
