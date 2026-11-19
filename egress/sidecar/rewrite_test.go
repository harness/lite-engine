package sidecar

import (
	"errors"
	"strings"
	"testing"
)

const testCred = "poc:pocpass"

var testAuth = "Proxy-Authorization: " + BasicAuth(testCred)

func TestRewriteExplicitConnectGetsCredentialAndKeepsHeaders(t *testing.T) {
	in := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	out, clientAuth, err := RewriteExplicit([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	want := "CONNECT example.com:443 HTTP/1.1\r\n" + testAuth + "\r\nHost: example.com:443\r\n\r\n"
	if clientAuth {
		t.Fatal("clientAuth = true, want false")
	}
	if string(out) != want {
		t.Fatalf("out =\n%q\nwant\n%q", out, want)
	}
}

func TestRewriteExplicitKeepsClientCredential(t *testing.T) {
	in := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic Y2xpZW50\r\n\r\n"
	out, clientAuth, err := RewriteExplicit([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	if !clientAuth {
		t.Fatal("clientAuth = false, want true")
	}
	if n := strings.Count(string(out), "Proxy-Authorization:"); n != 1 {
		t.Fatalf("Proxy-Authorization count = %d, want 1", n)
	}
	if !strings.Contains(string(out), "Proxy-Authorization: Basic Y2xpZW50") {
		t.Fatalf("client credential was replaced: %q", out)
	}
}

func TestRewriteExplicitPlainRequestForcesClose(t *testing.T) {
	in := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\nProxy-Connection: keep-alive\r\n\r\n"
	out, _, err := RewriteExplicit([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "keep-alive") {
		t.Fatalf("keep-alive survived: %q", s)
	}
	if n := strings.Count(s, "Connection: close"); n != 2 {
		t.Fatalf("Connection: close count = %d, want 2 (one replacing Connection, one replacing Proxy-Connection)", n)
	}
}

func TestRewriteExplicitPlainRequestAddsClose(t *testing.T) {
	in := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"
	out, _, err := RewriteExplicit([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "Host: example.com\r\nConnection: close\r\n\r\n") {
		t.Fatalf("Connection: close not appended: %q", out)
	}
}

func TestRewriteExplicitKeepsBodyBytes(t *testing.T) {
	in := "POST http://example.com/ HTTP/1.1\r\nHost: example.com\r\nContent-Length: 4\r\n\r\nBODY"
	out, _, err := RewriteExplicit([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "\r\n\r\nBODY") {
		t.Fatalf("body not preserved: %q", out)
	}
}

func TestRewriteExplicitIncompleteHead(t *testing.T) {
	if _, _, err := RewriteExplicit([]byte("GET / HTTP/1.1\r\nHost: x\r\n"), testCred); !errors.Is(err, ErrIncompleteHead) {
		t.Fatalf("error = %v, want ErrIncompleteHead", err)
	}
}

func TestRewriteAbsoluteBuildsURIAndForcesClose(t *testing.T) {
	in := "GET /releases/x.img HTTP/1.1\r\nHost: cloud-images.ubuntu.com\r\nUser-Agent: curl\r\n\r\n"
	out, clientAuth, err := RewriteAbsolute([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	want := "GET http://cloud-images.ubuntu.com/releases/x.img HTTP/1.1\r\n" +
		testAuth + "\r\n" +
		"Host: cloud-images.ubuntu.com\r\n" +
		"User-Agent: curl\r\n" +
		"Connection: close\r\n\r\n"
	if clientAuth {
		t.Fatal("clientAuth = true, want false")
	}
	if string(out) != want {
		t.Fatalf("out =\n%q\nwant\n%q", out, want)
	}
}

func TestRewriteAbsoluteKeepsAbsoluteTarget(t *testing.T) {
	in := "GET http://deb.debian.org/debian/dists/bookworm/Release HTTP/1.1\r\nHost: deb.debian.org\r\n\r\n"
	out, _, err := RewriteAbsolute([]byte(in), testCred)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(string(out), "GET http://deb.debian.org/debian/dists/bookworm/Release HTTP/1.1\r\n") {
		t.Fatalf("absolute target changed: %q", out)
	}
}

func TestRewriteAbsoluteWithoutHostIsRefused(t *testing.T) {
	if _, _, err := RewriteAbsolute([]byte("GET / HTTP/1.1\r\n\r\n"), testCred); !errors.Is(err, ErrNoHost) {
		t.Fatalf("error = %v, want ErrNoHost", err)
	}
}

func TestRewriteAbsoluteMalformedRequestLine(t *testing.T) {
	if _, _, err := RewriteAbsolute([]byte("GARBAGE\r\nHost: x\r\n\r\n"), testCred); !errors.Is(err, ErrMalformedRequestLine) {
		t.Fatalf("error = %v, want ErrMalformedRequestLine", err)
	}
}
