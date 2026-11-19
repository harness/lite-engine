// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

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
	connectOK  = "HTTP/1.1 200 Connection Established\r\n\r\n"
	proxyAuthH = "Proxy-Authorization: "
)

func TestTunnelUsesSNIOn443(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(connectOK))
		_, _ = io.Copy(io.Discard, c)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		if _, err := c.Write(buildClientHello(t, "pypi.org")); err != nil {
			t.Fatal(err)
		}
		_ = readAll(c)
	})
	got := proxy.request(t)
	if !bytes.HasPrefix(got, []byte("CONNECT pypi.org:443 HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if !bytes.Contains(got, []byte(proxyAuthH+basicAuth(testLogin()))) {
		t.Fatalf("missing credential: %q", got)
	}
	// The tunnel ends on the idle timeout, which is the only line expected.
	if got := logs.String(); strings.Contains(got, "upstream") || strings.Contains(got, "credential") {
		t.Fatalf("a successful tunnel logged %q", got)
	}
}

func TestTunnelWithoutSNIUsesOriginalDestination(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		if _, err := c.Write(buildClientHello(t, "")); err != nil {
			t.Fatal(err)
		}
	})
	if got := proxy.request(t); !bytes.HasPrefix(got, []byte("CONNECT 1.2.3.4:443 HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if !strings.Contains(logs.String(), "no SNI, using original destination 1.2.3.4:443") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestHostileSNIFallsBackToOriginalDestination(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
	srv := newTestServer(t, proxy, &lockedBuf{})
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello(t, "evil.example\r\nX-Injected: yes"))
	})
	got := proxy.request(t)
	if bytes.Contains(got, []byte("X-Injected")) || !bytes.HasPrefix(got, []byte("CONNECT 1.2.3.4:443 ")) {
		t.Fatalf("request = %q", got)
	}
}

func TestTLSOnPort80KeepsOriginalPort(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		if _, err := c.Write(buildClientHello(t, "example.com")); err != nil {
			t.Fatal(err)
		}
	})
	got := proxy.request(t)
	if !bytes.HasPrefix(got, []byte("CONNECT example.com:80 HTTP/1.1\r\n")) || bytes.Contains(got, []byte(":443")) {
		t.Fatalf("request = %q", got)
	}
	assertNoCredential(t, logs)
}

// A client that sent raw TLS does not expect an HTTP response, so the proxy's
// "200 Connection Established" must not reach it.
func TestTunnelDoesNotLeakConnectResponseToClient(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(connectOK + "SERVER-HELLO"))
	})
	srv := newTestServer(t, proxy, &lockedBuf{})
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello(t, "pypi.org"))
		if got := readAll(c); got != "SERVER-HELLO" {
			t.Fatalf("client received %q", got)
		}
	})
}

func TestTunnelForwardsClientBytesAfterConnect(t *testing.T) {
	var got bytes.Buffer
	var mu sync.Mutex
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(connectOK))
		data, _ := io.ReadAll(c)
		mu.Lock()
		got.Write(data)
		mu.Unlock()
	})
	srv := newTestServer(t, proxy, &lockedBuf{})
	hello := buildClientHello(t, "pypi.org")
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(hello)
		time.Sleep(100 * time.Millisecond)
	})
	_ = proxy.request(t)
	time.Sleep(100 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	if !bytes.Equal(got.Bytes(), hello) {
		t.Fatalf("proxy received %d bytes after CONNECT, want the %d-byte ClientHello", got.Len(), len(hello))
	}
}

func TestPlainHTTP80IsAbsoluteURI(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(okResponse("ok"))) })
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET /releases/x.img HTTP/1.1\r\nHost: cloud-images.ubuntu.com\r\n\r\n"))
		if got := readAll(c); !strings.Contains(got, "200 OK") || !strings.HasSuffix(got, "ok") {
			t.Fatalf("response = %q", got)
		}
	})
	got := proxy.request(t)
	if !bytes.HasPrefix(got, []byte("GET http://cloud-images.ubuntu.com/releases/x.img HTTP/1.1\r\n")) {
		t.Fatalf("request = %q", got)
	}
	if !bytes.Contains(got, []byte("Connection: close")) {
		t.Fatalf("missing close: %q", got)
	}
	assertNoCredential(t, logs)
}

// The proxy's answer to an explicit CONNECT is what the client is waiting for.
// Dropping it hangs every HTTPS tool that honors HTTPS_PROXY.
func TestExplicitConnectAnswerReachesClientAndTunnelWorks(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(connectOK))
		_, _ = io.Copy(c, c)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "172.16.0.11", proxy.port(t), func(c net.Conn) {
		_, _ = c.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
		if head := readHead(t, c); !strings.HasPrefix(head, "HTTP/1.1 200 ") {
			t.Fatalf("client received %q", head)
		}
		_, _ = c.Write([]byte("ping"))
		buf := make([]byte, 4)
		if _, err := io.ReadFull(c, buf); err != nil || string(buf) != "ping" {
			t.Fatalf("tunnel echo = %q, %v", buf, err)
		}
	})
	got := proxy.request(t)
	if !bytes.Contains(got, []byte(proxyAuthH+basicAuth(testLogin()))) {
		t.Fatalf("credential not added: %q", got)
	}
	if got := logs.String(); got != "" {
		t.Fatalf("a successful CONNECT logged %q", got)
	}
}

func TestExplicitConnectRefusalReachesClient(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 10\r\n\r\nblock"))
		time.Sleep(100 * time.Millisecond)
		_, _ = c.Write([]byte(" page"))
		// The proxy keeps the connection open; the sidecar must not wait for close.
		time.Sleep(testDeadline)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "172.16.0.11", proxy.port(t), func(c net.Conn) {
		_, _ = c.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"))
		start := time.Now()
		if got := readAll(c); !strings.HasPrefix(got, "HTTP/1.1 403 ") || !strings.HasSuffix(got, "\r\n\r\nblock page") {
			t.Fatalf("client received %q", got)
		}
		if waited := time.Since(start); waited > 2*time.Second {
			t.Fatalf("the refusal took %s; the sidecar waited for the proxy to close", waited)
		}
	})
	if !strings.Contains(logs.String(), "upstream rejected example.com:443: HTTP/1.1 403 Forbidden (credential source=sidecar)") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestExplicitProxyKeepsClientCredential(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "172.16.0.11", proxy.port(t), func(c net.Conn) {
		_, _ = c.Write([]byte("CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic Y2xpZW50\r\n\r\n"))
		_ = readHead(t, c)
	})
	got := proxy.request(t)
	if strings.Count(string(got), "Proxy-Authorization:") != 1 || !bytes.Contains(got, []byte("Proxy-Authorization: Basic Y2xpZW50")) {
		t.Fatalf("client credential was replaced or duplicated: %q", got)
	}
	if !strings.Contains(logs.String(), "500 Internal Server Error (credential source=client)") {
		t.Fatalf("logs = %s", logs.String())
	}
	assertNoCredential(t, logs)
}

func TestExplicitProxyAddsCredentialToPlainRequest(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(okResponse(""))) })
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "172.16.0.11", proxy.port(t), func(c net.Conn) {
		_, _ = c.Write([]byte("GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		_ = readAll(c)
	})
	if got := proxy.request(t); !bytes.Contains(got, []byte(proxyAuthH+basicAuth(testLogin()))) {
		t.Fatalf("credential not added: %q", got)
	}
	assertNoCredential(t, logs)
}

func TestSecondRequestIsNotForwarded(t *testing.T) {
	for _, tc := range []struct {
		name string
		port func(*fakeProxy) int
		req  string
	}{
		{
			name: "port 80",
			port: func(*fakeProxy) int { return 80 },
			req:  "GET /first HTTP/1.1\r\nHost: example.com\r\n\r\nGET /second HTTP/1.1\r\nHost: example.com\r\n\r\n",
		},
		{
			name: "proxy port",
			port: func(p *fakeProxy) int { return p.port(t) },
			req:  "GET http://example.com/first HTTP/1.1\r\nHost: example.com\r\n\r\nGET http://example.com/second HTTP/1.1\r\nHost: example.com\r\n\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			extra := make(chan []byte, 1)
			proxy := startProxyRaw(t, func(c net.Conn, reqs chan<- []byte) {
				reqs <- readProxyRequest(c)
				_, _ = c.Write([]byte(okResponse("one")))
				_ = c.SetReadDeadline(time.Now().Add(300 * time.Millisecond))
				rest, _ := io.ReadAll(c)
				extra <- rest
			})
			srv := newTestServer(t, proxy, &lockedBuf{})
			var response string
			runHandle(t, srv, "1.2.3.4", tc.port(proxy), func(c net.Conn) {
				_, _ = c.Write([]byte(tc.req))
				response = readAll(c)
			})
			first := proxy.request(t)
			if bytes.Contains(first, []byte("/second")) {
				t.Fatalf("second request was forwarded with the first: %q", first)
			}
			select {
			case rest := <-extra:
				if len(rest) != 0 {
					t.Fatalf("proxy later received %q", rest)
				}
			case <-time.After(testDeadline):
				t.Fatal("proxy did not finish")
			}
			if strings.Contains(response, "407") || !strings.Contains(response, "200 OK") {
				t.Fatalf("response = %q", response)
			}
		})
	}
}

func TestLargePostDoesNotWaitForResponse(t *testing.T) {
	const size = 256 * 1024
	for _, tc := range []struct {
		name string
		port func(*fakeProxy) int
		head string
	}{
		{
			name: "port 80",
			port: func(*fakeProxy) int { return 80 },
			head: "POST /upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 262144\r\n\r\n",
		},
		{
			name: "proxy port",
			port: func(p *fakeProxy) int { return p.port(t) },
			head: "POST http://example.com/upload HTTP/1.1\r\nHost: example.com\r\nContent-Length: 262144\r\n\r\n",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			sawHead := make(chan struct{})
			proxy := startProxyRaw(t, func(c net.Conn, reqs chan<- []byte) {
				var buf []byte
				tmp := make([]byte, readChunkSize)
				for !bytes.Contains(buf, crlfcrlf) {
					n, err := c.Read(tmp)
					buf = append(buf, tmp[:n]...)
					if err != nil {
						return
					}
				}
				close(sawHead)
				head, body, _ := splitHead(buf)
				reqs <- head
				if left := size - len(body); left > 0 {
					if _, err := io.CopyN(io.Discard, c, int64(left)); err != nil {
						return
					}
				}
				_, _ = c.Write([]byte("HTTP/1.1 201 Created\r\nContent-Length: 4\r\nConnection: close\r\n\r\nsent"))
			})
			srv := newTestServer(t, proxy, &lockedBuf{})
			var response string
			runHandle(t, srv, "1.2.3.4", tc.port(proxy), func(c net.Conn) {
				_, _ = c.Write([]byte(tc.head))
				select {
				case <-sawHead:
				case <-time.After(testDeadline):
					t.Fatal("the proxy did not see the request head before the body was sent")
				}
				_, _ = c.Write(bytes.Repeat([]byte("x"), size))
				response = readAll(c)
			})
			if !strings.Contains(response, "201 Created") || !strings.HasSuffix(response, "sent") {
				t.Fatalf("response = %q", response)
			}
		})
	}
}

func TestChunkedRequestIsForwardedWholeAndNoFurther(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(okResponse("done"))) })
	srv := newTestServer(t, proxy, &lockedBuf{})
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("POST /up HTTP/1.1\r\nHost: example.com\r\nTransfer-Encoding: chunked\r\n\r\n5\r\nhel"))
		time.Sleep(50 * time.Millisecond)
		_, _ = c.Write([]byte("lo\r"))
		time.Sleep(50 * time.Millisecond)
		_, _ = c.Write([]byte("\n0\r\n\r\nGET /second HTTP/1.1\r\n\r\n"))
		if got := readAll(c); !strings.HasSuffix(got, "done") {
			t.Fatalf("response = %q", got)
		}
	})
	got := string(proxy.request(t))
	if !strings.HasSuffix(got, "5\r\nhello\r\n0\r\n\r\n") {
		t.Fatalf("request = %q", got)
	}
}

func TestExpectContinueReachesClientBeforeBody(t *testing.T) {
	proxy := startProxyRaw(t, func(c net.Conn, reqs chan<- []byte) {
		var buf []byte
		tmp := make([]byte, readChunkSize)
		for !bytes.Contains(buf, crlfcrlf) {
			n, err := c.Read(tmp)
			buf = append(buf, tmp[:n]...)
			if err != nil {
				return
			}
		}
		reqs <- buf
		_, _ = c.Write([]byte("HTTP/1.1 100 Continue\r\n\r\n"))
		body := make([]byte, 5)
		if _, err := io.ReadFull(c, body); err != nil {
			return
		}
		_, _ = c.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("PUT /x HTTP/1.1\r\nHost: example.com\r\nExpect: 100-continue\r\nContent-Length: 5\r\n\r\n"))
		if head := readHead(t, c); !strings.HasPrefix(head, "HTTP/1.1 100 ") {
			t.Fatalf("client received %q before sending the body", head)
		}
		_, _ = c.Write([]byte("hello"))
		if got := readAll(c); !strings.HasPrefix(got, "HTTP/1.1 500 ") {
			t.Fatalf("final response = %q", got)
		}
	})
	_ = proxy.request(t)
	if got := logs.String(); !strings.Contains(got, "upstream response example.com: HTTP/1.1 500") || strings.Contains(got, "100 Continue") {
		t.Fatalf("logs = %s", got)
	}
}

func TestWrongTokenStatusIsPassedThrough(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 4\r\nConnection: close\r\n\r\nnope"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	var response string
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		response = readAll(c)
	})
	if !strings.Contains(response, "500") || !strings.Contains(response, "nope") {
		t.Fatalf("response = %q", response)
	}
	if got := logs.String(); !strings.Contains(got, "500") || !strings.Contains(got, "credential source=sidecar") {
		t.Fatalf("logs = %s", got)
	}
	assertNoCredential(t, logs)
}

func TestLogsDoNotIncludePathOrQuery(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 500 Internal Server Error\r\nContent-Length: 0\r\nConnection: close\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET /download?token=customer-secret HTTP/1.1\r\nHost: files.example.com\r\n\r\n"))
		_ = readAll(c)
	})
	got := logs.String()
	if !strings.Contains(got, "files.example.com") {
		t.Fatalf("host missing from logs: %s", got)
	}
	if strings.Contains(got, "customer-secret") || strings.Contains(got, "/download") {
		t.Fatalf("logs include the request path or query: %s", got)
	}
}

func TestRejectedConnectClosesTransparentClient(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 403 Forbidden\r\nContent-Length: 0\r\n\r\n"))
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	runHandle(t, srv, "9.9.9.9", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello(t, "blocked.example"))
		if got := readAll(c); got != "" {
			t.Fatalf("client received %q, want a closed connection", got)
		}
	})
	if got := logs.String(); !strings.Contains(got, "upstream rejected blocked.example:443") || !strings.Contains(got, "403") {
		t.Fatalf("logs = %s", got)
	}
	assertNoCredential(t, logs)
}

func TestIdleTimeoutClosesQuietTunnel(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(connectOK))
		_, _ = io.Copy(io.Discard, c)
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	srv.idle = 200 * time.Millisecond
	start := time.Now()
	runHandle(t, srv, "1.2.3.4", 443, func(c net.Conn) {
		_, _ = c.Write(buildClientHello(t, "pypi.org"))
		_ = readAll(c)
	})
	if time.Since(start) > 3*time.Second {
		t.Fatalf("quiet tunnel stayed open for %s", time.Since(start))
	}
	if !strings.Contains(logs.String(), "idle timeout") {
		t.Fatalf("logs = %s", logs.String())
	}
}

// A peer that stops reading must not pin the relay forever.
func TestStalledClientIsDropped(t *testing.T) {
	const size = 64 << 20
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte("HTTP/1.1 200 OK\r\nContent-Length: 67108864\r\nConnection: close\r\n\r\n"))
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for sent := 0; sent < size; sent += len(chunk) {
			if _, err := c.Write(chunk); err != nil {
				return
			}
		}
	})
	logs := &lockedBuf{}
	srv := newTestServer(t, proxy, logs)
	srv.idle = 300 * time.Millisecond
	client, done := startHandler(t, srv, "1.2.3.4", 80)
	if _, err := client.Write([]byte("GET /big HTTP/1.1\r\nHost: example.com\r\n\r\n")); err != nil {
		t.Fatal(err)
	}
	waitDone(t, done)
	if !strings.Contains(logs.String(), "idle timeout") {
		t.Fatalf("logs = %s", logs.String())
	}
}

// A reader that pauses for less than the idle timeout still gets the whole file.
func TestSlowReaderUnderIdleKeepsTransfer(t *testing.T) {
	const size = 24 << 20
	head := "HTTP/1.1 200 OK\r\nContent-Length: 25165824\r\nConnection: close\r\n\r\n"
	proxy := startProxy(t, func(c net.Conn, _ []byte) {
		_, _ = c.Write([]byte(head))
		chunk := bytes.Repeat([]byte("x"), 1<<20)
		for sent := 0; sent < size; sent += len(chunk) {
			if _, err := c.Write(chunk); err != nil {
				return
			}
		}
	})
	srv := newTestServer(t, proxy, &lockedBuf{})
	srv.idle = 3 * time.Second
	runHandle(t, srv, "1.2.3.4", 80, func(c net.Conn) {
		_, _ = c.Write([]byte("GET /big HTTP/1.1\r\nHost: example.com\r\n\r\n"))
		one := make([]byte, 1)
		if _, err := io.ReadFull(c, one); err != nil {
			t.Fatal(err)
		}
		time.Sleep(500 * time.Millisecond)
		n, _ := io.Copy(io.Discard, c)
		if total := int(n) + 1; total != len(head)+size {
			t.Fatalf("received %d bytes, want %d", total, len(head)+size)
		}
	})
}

func newLoopbackServer(t *testing.T, proxy *fakeProxy, logs *lockedBuf, dst func(net.Conn) (string, int, error)) string {
	t.Helper()
	srv := newTestServer(t, proxy, logs)
	srv.originalDst = dst
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = srv.Serve(ln) }()
	t.Cleanup(func() { _ = srv.Close() })
	return ln.Addr().String()
}

func TestServeRelaysThroughOriginalDestination(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(okResponse("hello"))) })
	logs := &lockedBuf{}
	addr := newLoopbackServer(t, proxy, logs, func(net.Conn) (string, int, error) { return "1.2.3.4", 80, nil })
	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(testDeadline))
	_, _ = client.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	if got := readAll(client); !strings.HasSuffix(got, "hello") {
		t.Fatalf("response = %q", got)
	}
}

// One panicking connection must not stop the sidecar from serving the next.
func TestPanicInOneConnectionDoesNotStopServer(t *testing.T) {
	var calls int
	var mu sync.Mutex
	dst := func(net.Conn) (string, int, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 1 {
			panic("simulated bug")
		}
		return "1.2.3.4", 80, nil
	}

	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(okResponse("alive"))) })
	logs := &lockedBuf{}
	addr := newLoopbackServer(t, proxy, logs, dst)

	first, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	_ = first.SetDeadline(time.Now().Add(testDeadline))
	_ = readAll(first) // closed by the sidecar after the recovered panic
	_ = first.Close()

	second, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer second.Close()
	_ = second.SetDeadline(time.Now().Add(testDeadline))
	_, _ = second.Write([]byte("GET / HTTP/1.1\r\nHost: example.com\r\n\r\n"))
	if got := readAll(second); !strings.HasSuffix(got, "alive") {
		t.Fatalf("second connection response = %q", got)
	}
	if !strings.Contains(logs.String(), "connection handler panic") {
		t.Fatalf("logs = %s", logs.String())
	}
}

func TestAcceptRetries(t *testing.T) {
	logs := &lockedBuf{}
	srv, err := New(&Config{
		ProxyHost:  "127.0.0.1",
		ProxyPort:  3128,
		Credential: testLogin(),
		Log:        log.New(logs, "", 0),
	})
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- srv.Serve(&failOnce{Listener: ln}) }()

	deadline := time.Now().Add(testDeadline)
	for !strings.Contains(logs.String(), "accept failed") {
		if time.Now().After(deadline) {
			t.Fatalf("Serve never logged the accept failure: %s", logs.String())
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := ln.Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(testDeadline):
		t.Fatal("Serve did not return after the listener closed")
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

// A connection made straight to the listener reports the listener as its
// original destination. It must not be relayed with the customer's credential.
func TestConnectionThatWasNotRedirectedIsRefused(t *testing.T) {
	proxy := startProxy(t, func(c net.Conn, _ []byte) { _, _ = c.Write([]byte(connectOK)) })
	logs := &lockedBuf{}
	addr := newLoopbackServer(t, proxy, logs, func(c net.Conn) (string, int, error) {
		la := c.LocalAddr().(*net.TCPAddr)
		return la.IP.String(), la.Port, nil
	})
	client, err := net.Dial("tcp", addr)
	if err != nil {
		t.Fatal(err)
	}
	defer client.Close()
	_ = client.SetDeadline(time.Now().Add(testDeadline))
	_, _ = client.Write(buildClientHello(t, "anything.attacker.example"))
	if got := readAll(client); got != "" {
		t.Fatalf("client received %q", got)
	}
	select {
	case req := <-proxy.reqs:
		t.Fatalf("the proxy received %q", req)
	case <-time.After(200 * time.Millisecond):
	}
	if !strings.Contains(logs.String(), "that was not redirected") {
		t.Fatalf("logs = %s", logs.String())
	}
}
