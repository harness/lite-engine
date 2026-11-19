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

func (p *transportProxy) proxyCleared() bool { return p.t.Proxy == nil }

func (p *transportProxy) dialSet() bool { return p.t.DialContext != nil }

// The default HTTP transport is created with its own dialer. Apply has to
// replace that dialer, or the socket mark is never set.
func TestApplyReplacesAnExistingDialer(t *testing.T) {
	t.Setenv(envEnabled, "true")
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
