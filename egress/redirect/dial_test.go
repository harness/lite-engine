// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"testing"
	"time"
)

// transportProxy is a small stand-in so the test reads as the check it makes.
type transportProxy struct {
	t *http.Transport
}

func (p *transportProxy) transport() *http.Transport {
	p.t = &http.Transport{Proxy: http.ProxyFromEnvironment}
	return p.t
}

func (p *transportProxy) usesEnvironmentProxy() bool { return p.t.Proxy != nil }

// The default HTTP transport is created with its own dialer. Apply has to
// replace that dialer, or the socket mark is never set.
func TestApplyReplacesAnExistingDialer(t *testing.T) {
	enable(t)
	used := false
	transport := &http.Transport{
		Proxy: http.ProxyFromEnvironment,
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			used = true
			return nil, errors.New("old dialer")
		},
	}
	Apply(transport)

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	_ = ln.Close()

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	_, err = transport.DialContext(ctx, "tcp", addr)
	if used {
		t.Fatal("the dialer that was already installed was used")
	}
	if err == nil || err.Error() == "old dialer" {
		t.Fatalf("dial error = %v", err)
	}
}

func TestApplyKeepsTheExistingDialerWhenOff(t *testing.T) {
	used := false
	transport := &http.Transport{
		DialContext: func(context.Context, string, string) (net.Conn, error) {
			used = true
			return nil, errors.New("old dialer")
		},
	}
	Apply(transport)
	_, err := transport.DialContext(context.Background(), "tcp", "127.0.0.1:1")
	if !used || err == nil || err.Error() != "old dialer" {
		t.Fatalf("used=%v err=%v", used, err)
	}
}

func TestApplySetsDirectDialOnlyWhenEnabled(t *testing.T) {
	off := &transportProxy{}
	Apply(off.transport())
	if !off.usesEnvironmentProxy() || off.t.DialContext != nil {
		t.Fatal("Apply changed the transport while the redirect is off")
	}
	enable(t)
	on := &transportProxy{}
	Apply(on.transport())
	if on.t.DialContext == nil {
		t.Fatal("Apply did not install a dialer")
	}
}

// The proxy is ignored only while the redirect is installed, so a stage that
// does not use it still reaches the log service through a secure-tunnel proxy.
func TestApplyIgnoresTheProxyOnlyWhileInstalled(t *testing.T) {
	enable(t)
	transport := &http.Transport{Proxy: func(*http.Request) (*url.URL, error) {
		return url.Parse("http://tunnel.corp:3128")
	}}
	Apply(transport)
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, "https://logs.example.com", http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	got, err := transport.Proxy(req)
	if err != nil || got == nil {
		t.Fatalf("proxy before install = %v, %v", got, err)
	}
	setActive("172.16.0.11", 3128)
	got, err = transport.Proxy(req)
	if err != nil || got != nil {
		t.Fatalf("proxy while installed = %v, %v", got, err)
	}
}

func TestApplyClientHandlesAClientWithoutATransport(t *testing.T) {
	enable(t)
	ApplyClient(nil)
	ApplyClient(&http.Client{})
	client := &http.Client{Transport: &http.Transport{}}
	ApplyClient(client)
	if client.Transport.(*http.Transport).DialContext == nil {
		t.Fatal("ApplyClient did not install a dialer")
	}
}
