// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"net"
	"net/http"
	"net/url"
	"time"
)

const (
	dialTimeout = 30 * time.Second
	dialAlive   = 30 * time.Second
)

// Arm exempts the default HTTP transport from the redirect. Call it after the
// environment file is loaded and before the server accepts. Clients a package
// created itself are marked by that package.
func Arm() {
	if transport, ok := http.DefaultTransport.(*http.Transport); ok {
		Apply(transport)
	}
}

// Dialer is a dialer whose sockets carry the mark while the redirect is on.
// The control function is a no-op while the redirect is off.
func Dialer(timeout, keepAlive time.Duration) *net.Dialer {
	return &net.Dialer{Timeout: timeout, KeepAlive: keepAlive, Control: DialControl}
}

// Apply exempts transport from the redirect. Its sockets carry the mark, so the
// nat rules leave them alone, and it ignores a proxy while the redirect is
// installed. A dialer the transport already had is replaced: the default
// transport is created with one, and leaving it in place would send unmarked
// sockets into the redirect. With CI_EGRESS_SIDECAR unset it changes nothing.
func Apply(transport *http.Transport) {
	if transport == nil || !Enabled() {
		return
	}
	proxy := transport.Proxy
	transport.Proxy = func(req *http.Request) (*url.URL, error) {
		if proxy == nil || Active() {
			return nil, nil //nolint:nilnil // a nil URL is how a transport is told to dial directly
		}
		return proxy(req)
	}
	transport.DialContext = Dialer(dialTimeout, dialAlive).DialContext
}

// ApplyClient exempts client's transport from the redirect, for a client that a
// library built and that lite-engine cannot configure otherwise.
func ApplyClient(client *http.Client) {
	if client == nil {
		return
	}
	if transport, ok := client.Transport.(*http.Transport); ok {
		Apply(transport)
	}
}
