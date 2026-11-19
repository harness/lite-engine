package sidecar

import (
	"errors"
	"log"
	"net"
	"os"
	"strconv"
	"time"
)

var (
	errProxyHostRequired  = errors.New("UPSTREAM_PROXY_HOST is required")
	errProxyPortInvalid   = errors.New("UPSTREAM_PROXY_PORT is invalid")
	errListenPortInvalid  = errors.New("LISTEN_PORT is invalid")
	errIdleTimeoutInvalid = errors.New("IDLE_TIMEOUT is invalid")
)

const (
	// DefaultListenAddr is the redirect target from the POC. CI-26163 points
	// iptables at this port.
	DefaultListenAddr = ":15001"
	// DefaultProxyPort is the fleet proxy's HTTP listener.
	DefaultProxyPort = 3128
	// DefaultIdleTimeout closes a connection that has had no bytes in either
	// direction. The POC used 30s. Five minutes covers a quiet keep-alive
	// without holding a dead connection open. A transfer blocked on a slow
	// reader is not cut off by this timer.
	DefaultIdleTimeout = 5 * time.Minute
	// DefaultDialTimeout bounds the connection to the fleet proxy.
	DefaultDialTimeout = 8 * time.Second
	// DefaultFirstRead bounds the wait for the first client bytes.
	DefaultFirstRead = 3 * time.Second
	// DefaultHeaderTimeout bounds the wait for a CONNECT response. It is not
	// applied to a request body.
	DefaultHeaderTimeout = 8 * time.Second
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

func (c Config) withDefaults() Config {
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
	return c
}

// ConfigFromEnv reads the process environment. PROXY_AUTH wins over
// PROXY_AUTH_FILE. A missing or incomplete credential returns
// ErrCredentialIncomplete and the value is not included in the error.
func ConfigFromEnv() (Config, error) {
	var credential string
	if raw := os.Getenv("PROXY_AUTH"); raw != "" {
		parsed, err := ParseCredential(raw)
		if err != nil {
			return Config{}, err
		}
		credential = parsed
	} else {
		path := os.Getenv("PROXY_AUTH_FILE")
		if path == "" {
			return Config{}, ErrCredentialIncomplete
		}
		parsed, err := ReadCredentialFile(path)
		if err != nil {
			return Config{}, err
		}
		credential = parsed
	}
	host := os.Getenv("UPSTREAM_PROXY_HOST")
	if host == "" {
		return Config{}, errProxyHostRequired
	}
	port := DefaultProxyPort
	if raw := os.Getenv("UPSTREAM_PROXY_PORT"); raw != "" {
		parsed, err := strconv.Atoi(raw)
		if err != nil || parsed <= 0 || parsed > 65535 {
			return Config{}, errProxyPortInvalid
		}
		port = parsed
	}
	listen := os.Getenv("LISTEN_ADDR")
	if listen == "" {
		if raw := os.Getenv("LISTEN_PORT"); raw != "" {
			if _, err := strconv.Atoi(raw); err != nil {
				return Config{}, errListenPortInvalid
			}
			listen = net.JoinHostPort("", raw)
		}
	}
	idle := DefaultIdleTimeout
	if raw := os.Getenv("IDLE_TIMEOUT"); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil || parsed <= 0 {
			return Config{}, errIdleTimeoutInvalid
		}
		idle = parsed
	}
	return Config{
		ListenAddr:  listen,
		ProxyHost:   host,
		ProxyPort:   port,
		Credential:  credential,
		IdleTimeout: idle,
	}, nil
}
