// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package redirect

import (
	"net"
	"os"
	"runtime"
	"strconv"
	"strings"
	"sync"
)

const envEnabled = "CI_EGRESS_SIDECAR"

// hostOS is runtime.GOOS. A test sets it to run the Linux paths elsewhere.
var hostOS = runtime.GOOS

// proxyURLEnvNames hold a proxy URL. One that points at the fleet proxy loses
// its credential, so the step does not carry it in its environment. A proxy that
// is not the fleet proxy, such as a customer's, is left alone.
var proxyURLEnvNames = []string{
	"http_proxy", "https_proxy", "all_proxy",
	"HTTP_PROXY", "HTTPS_PROXY", "ALL_PROXY",
}

// active is the redirect that is installed now. It is set by Install and cleared
// by Remove, so clients and steps change behavior only while it is in place.
var active struct {
	sync.RWMutex
	on   bool
	host string
	port int
}

// Enabled reports whether this process may install the redirect. The runner sets
// CI_EGRESS_SIDECAR. It is unset, and the redirect is off, otherwise. The
// redirect is Linux only: iptables and SO_MARK do not exist elsewhere.
func Enabled() bool {
	if hostOS != "linux" {
		return false
	}
	switch strings.TrimSpace(os.Getenv(envEnabled)) {
	case "1", "true", "TRUE", "True":
		return true
	default:
		return false
	}
}

// Applies reports whether a stage with this proxy URL uses the redirect. A stage
// without a fleet proxy, or one joined to a private network, does not.
func Applies(proxyURL string, privateConnectivity bool) bool {
	return Enabled() && !privateConnectivity && strings.TrimSpace(proxyURL) != ""
}

// Active reports whether the redirect is installed.
func Active() bool {
	active.RLock()
	defer active.RUnlock()
	return active.on
}

func setActive(host string, port int) {
	active.Lock()
	defer active.Unlock()
	active.on, active.host, active.port = true, host, port
}

func clearActive() {
	active.Lock()
	defer active.Unlock()
	active.on, active.host, active.port = false, "", 0
}

func activeProxy() (host string, port int, ok bool) {
	active.RLock()
	defer active.RUnlock()
	return active.host, active.port, active.on
}

// Scrub rewrites the fleet proxy in env to its address without the credential,
// while the redirect is installed. env is the map that will be copied into a
// step.
func Scrub(env map[string]string) {
	if host, port, ok := activeProxy(); ok {
		scrubProxy(env, host, port)
	}
}

// ScrubFor does the same before the redirect is installed, for the proxy at
// proxyURL, when this stage will use the redirect.
func ScrubFor(env map[string]string, proxyURL string, privateConnectivity bool) {
	if !Applies(proxyURL, privateConnectivity) {
		return
	}
	if host, port, err := parseProxy(proxyURL); err == nil {
		scrubProxy(env, host, port)
	}
}

// scrubProxy keeps a tool that honors a proxy variable on the proxy, so ports
// the redirect does not cover are still proxied, and drops the credential. The
// request goes to the proxy address, which the redirect sends to the sidecar,
// and the sidecar adds the credential.
func scrubProxy(env map[string]string, host string, port int) {
	for _, name := range proxyURLEnvNames {
		if isFleetProxy(env[name], host, port) {
			env[name] = proxyAddress(host, port)
		}
	}
}

func proxyAddress(host string, port int) string {
	return "http://" + net.JoinHostPort(host, strconv.Itoa(port))
}

func isFleetProxy(value, host string, port int) bool {
	if value == "" {
		return false
	}
	h, p, err := parseProxy(value)
	return err == nil && strings.EqualFold(h, host) && p == port
}

// scrubProcessProxy removes the credential from the fleet proxy in this
// process's environment, so programs that lite-engine starts do not carry it.
func scrubProcessProxy(host string, port int) {
	for _, name := range proxyURLEnvNames {
		if isFleetProxy(os.Getenv(name), host, port) {
			_ = os.Setenv(name, proxyAddress(host, port))
		}
	}
}
