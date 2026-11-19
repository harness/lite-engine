// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"strings"
	"testing"
)

const rewriteLogin = "user:value"

var rewriteAuth = "Proxy-Authorization: " + basicAuth(rewriteLogin)

func TestRewriteExplicitConnectGetsCredentialAndKeepsHeaders(t *testing.T) {
	in := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\n\r\n"
	out, clientAuth, err := rewriteExplicit([]byte(in), rewriteLogin)
	if err != nil {
		t.Fatal(err)
	}
	want := "CONNECT example.com:443 HTTP/1.1\r\n" + rewriteAuth + "\r\nHost: example.com:443\r\n\r\n"
	if clientAuth || string(out) != want {
		t.Fatalf("clientAuth=%v out=\n%q\nwant\n%q", clientAuth, out, want)
	}
}

func TestRewriteExplicitKeepsClientCredential(t *testing.T) {
	in := "CONNECT example.com:443 HTTP/1.1\r\nHost: example.com:443\r\nProxy-Authorization: Basic Y2xpZW50\r\n\r\n"
	out, clientAuth, err := rewriteExplicit([]byte(in), rewriteLogin)
	if err != nil {
		t.Fatal(err)
	}
	if !clientAuth || strings.Count(string(out), "Proxy-Authorization:") != 1 || !strings.Contains(string(out), "Basic Y2xpZW50") {
		t.Fatalf("client credential was replaced or duplicated: %q", out)
	}
}

func TestRewriteExplicitPlainRequestForcesClose(t *testing.T) {
	in := "GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\nConnection: keep-alive\r\nProxy-Connection: keep-alive\r\n\r\n"
	out, _, err := rewriteExplicit([]byte(in), rewriteLogin)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "keep-alive") || strings.Count(s, "Connection: close") != 2 {
		t.Fatalf("Connection headers = %q", s)
	}
}

func TestRewriteExplicitPlainRequestAddsClose(t *testing.T) {
	out, _, err := rewriteExplicit([]byte("GET http://example.com/ HTTP/1.1\r\nHost: example.com\r\n\r\n"), rewriteLogin)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(string(out), "Host: example.com\r\nConnection: close\r\n\r\n") {
		t.Fatalf("Connection: close not appended: %q", out)
	}
}

func TestRewriteExplicitKeepsBodyBytes(t *testing.T) {
	in := "POST http://example.com/ HTTP/1.1\r\nHost: example.com\r\nContent-Length: 4\r\n\r\nBODY"
	out, _, err := rewriteExplicit([]byte(in), rewriteLogin)
	if err != nil || !strings.HasSuffix(string(out), "\r\n\r\nBODY") {
		t.Fatalf("body not preserved: %q, %v", out, err)
	}
}

func TestRewriteExplicitIncompleteHead(t *testing.T) {
	if _, _, err := rewriteExplicit([]byte("GET / HTTP/1.1\r\nHost: x\r\n"), rewriteLogin); !errors.Is(err, errIncompleteHead) {
		t.Fatalf("error = %v, want errIncompleteHead", err)
	}
}

func TestRewriteAbsoluteBuildsURIAndForcesClose(t *testing.T) {
	in := "GET /releases/x.img HTTP/1.1\r\nHost: cloud-images.ubuntu.com\r\nUser-Agent: curl\r\n\r\n"
	out, clientAuth, err := rewriteAbsolute([]byte(in), rewriteLogin)
	if err != nil {
		t.Fatal(err)
	}
	want := "GET http://cloud-images.ubuntu.com/releases/x.img HTTP/1.1\r\n" +
		rewriteAuth + "\r\n" +
		"Host: cloud-images.ubuntu.com\r\n" +
		"User-Agent: curl\r\n" +
		"Connection: close\r\n\r\n"
	if clientAuth || string(out) != want {
		t.Fatalf("clientAuth=%v out=\n%q\nwant\n%q", clientAuth, out, want)
	}
}

func TestRewriteAbsoluteKeepsAbsoluteTarget(t *testing.T) {
	in := "GET http://deb.debian.org/debian/dists/bookworm/Release HTTP/1.1\r\nHost: deb.debian.org\r\n\r\n"
	out, _, err := rewriteAbsolute([]byte(in), rewriteLogin)
	if err != nil || !strings.HasPrefix(string(out), "GET http://deb.debian.org/debian/dists/bookworm/Release HTTP/1.1\r\n") {
		t.Fatalf("absolute target changed: %q, %v", out, err)
	}
}

func TestRewriteAbsoluteWithoutHostIsRefused(t *testing.T) {
	if _, _, err := rewriteAbsolute([]byte("GET / HTTP/1.1\r\n\r\n"), rewriteLogin); !errors.Is(err, errNoHost) {
		t.Fatalf("error = %v, want errNoHost", err)
	}
}

func TestRewriteAbsoluteMalformedRequestLine(t *testing.T) {
	if _, _, err := rewriteAbsolute([]byte("GARBAGE\r\nHost: x\r\n\r\n"), rewriteLogin); !errors.Is(err, errMalformedRequestLine) {
		t.Fatalf("error = %v, want errMalformedRequestLine", err)
	}
}
