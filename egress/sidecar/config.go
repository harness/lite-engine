// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"log"
	"os"
	"strconv"
	"time"
)

const (
	// DefaultListenAddr is the redirect target from the POC. CI-26163 points
	// iptables at this port.
	DefaultListenAddr = ":15001"
	// DefaultProxyPort is the fleet proxy's HTTP listener.
	DefaultProxyPort = 3128
	// DefaultIdleTimeout closes a relay when no bytes move in either direction
	// for this long. The POC used 30s. Five minutes covers a quiet keep-alive
	// without holding a dead connection open. A peer that reads slowly but
	// steadily is not cut off by it.
	DefaultIdleTimeout = 5 * time.Minute
	// DefaultDialTimeout bounds the connection to the fleet proxy.
	DefaultDialTimeout = 8 * time.Second
	// DefaultFirstRead bounds the wait for the first client bytes.
	DefaultFirstRead = 3 * time.Second
	// DefaultHeaderTimeout bounds the wait for a CONNECT response. It is not
	// applied to a request body.
	DefaultHeaderTimeout = 8 * time.Second

	maxPort = 65535

	envProxyAuth     = "PROXY_AUTH"
	envProxyAuthFile = "PROXY_AUTH_FILE"
	envProxyHost     = "UPSTREAM_PROXY_HOST"
	envProxyPort     = "UPSTREAM_PROXY_PORT"
	envListenAddr    = "LISTEN_ADDR"
	envListenPort    = "LISTEN_PORT"
	envIdleTimeout   = "IDLE_TIMEOUT"
)

var (
	errProxyHostRequired  = errors.New(envProxyHost + " is required")
	errProxyPortInvalid   = errors.New(envProxyPort + " is invalid")
	errListenPortInvalid  = errors.New(envListenPort + " is invalid")
	errIdleTimeoutInvalid = errors.New(envIdleTimeout + " is invalid")
)

// Config is everything the process needs. The credential is read once, before
// New returns, and is never logged.
type Config struct {
	ListenAddr    string
	ProxyHost     string
	ProxyPort     int
	Credential    string
	IdleTimeout   time.Duration
	DialTimeout   time.Duration
	FirstRead     time.Duration
	HeaderTimeout time.Duration
	Log           *log.Logger
}

func (c *Config) applyDefaults() {
	if c.ListenAddr == "" {
		c.ListenAddr = DefaultListenAddr
	}
	if c.ProxyPort == 0 {
		c.ProxyPort = DefaultProxyPort
	}
	if c.IdleTimeout == 0 {
		c.IdleTimeout = DefaultIdleTimeout
	}
	if c.DialTimeout == 0 {
		c.DialTimeout = DefaultDialTimeout
	}
	if c.FirstRead == 0 {
		c.FirstRead = DefaultFirstRead
	}
	if c.HeaderTimeout == 0 {
		c.HeaderTimeout = DefaultHeaderTimeout
	}
	if c.Log == nil {
		c.Log = log.New(os.Stderr, "egress-sidecar: ", log.LstdFlags)
	}
}

// ConfigFromEnv reads the process environment. A missing or incomplete
// credential returns ErrCredentialIncomplete, and the value is never part of
// an error.
func ConfigFromEnv() (Config, error) {
	credential, err := credentialFromEnv()
	if err != nil {
		return Config{}, err
	}
	host := os.Getenv(envProxyHost)
	if host == "" {
		return Config{}, errProxyHostRequired
	}
	port, err := portFromEnv(envProxyPort, DefaultProxyPort, errProxyPortInvalid)
	if err != nil {
		return Config{}, err
	}
	listen, err := listenAddrFromEnv()
	if err != nil {
		return Config{}, err
	}
	idle, err := idleTimeoutFromEnv()
	if err != nil {
		return Config{}, err
	}
	return Config{
		ListenAddr:  listen,
		ProxyHost:   host,
		ProxyPort:   port,
		Credential:  credential,
		IdleTimeout: idle,
	}, nil
}

// credentialFromEnv reads PROXY_AUTH, or the file named by PROXY_AUTH_FILE. The
// file is the production path. PROXY_AUTH exists for the POC and for tests.
func credentialFromEnv() (string, error) {
	if raw := os.Getenv(envProxyAuth); raw != "" {
		return ParseCredential(raw)
	}
	path := os.Getenv(envProxyAuthFile)
	if path == "" {
		return "", ErrCredentialIncomplete
	}
	return readCredentialFile(path)
}

func portFromEnv(name string, fallback int, invalid error) (int, error) {
	raw := os.Getenv(name)
	if raw == "" {
		return fallback, nil
	}
	port, err := strconv.Atoi(raw)
	if err != nil || port <= 0 || port > maxPort {
		return 0, invalid
	}
	return port, nil
}

// listenAddrFromEnv returns LISTEN_ADDR, else ":" plus LISTEN_PORT, else "" so
// the default applies.
func listenAddrFromEnv() (string, error) {
	if addr := os.Getenv(envListenAddr); addr != "" {
		return addr, nil
	}
	port, err := portFromEnv(envListenPort, 0, errListenPortInvalid)
	if err != nil || port == 0 {
		return "", err
	}
	return ":" + strconv.Itoa(port), nil
}

func idleTimeoutFromEnv() (time.Duration, error) {
	raw := os.Getenv(envIdleTimeout)
	if raw == "" {
		return DefaultIdleTimeout, nil
	}
	idle, err := time.ParseDuration(raw)
	if err != nil || idle <= 0 {
		return 0, errIdleTimeoutInvalid
	}
	return idle, nil
}
