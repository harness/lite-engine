// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// setEnv sets every variable the sidecar reads, so a test starts from a known
// environment whatever the host has set.
func setEnv(t *testing.T, overrides map[string]string) {
	t.Helper()
	for _, name := range []string{
		envProxyAuth, envProxyAuthFile, envProxyHost, envProxyPort, envListenAddr, envListenPort, envIdleTimeout,
	} {
		t.Setenv(name, overrides[name])
	}
}

func TestConfigFromEnvReadsCredentialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("acct|org:value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	setEnv(t, map[string]string{
		envProxyAuthFile: path,
		envProxyHost:     "172.16.0.11",
		envListenPort:    "15001",
		envIdleTimeout:   "90s",
	})
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Credential != "acct|org:value" || cfg.ProxyHost != "172.16.0.11" || cfg.ProxyPort != DefaultProxyPort {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.ListenAddr != ":15001" || cfg.IdleTimeout != 90*time.Second {
		t.Fatalf("listen=%q idle=%s", cfg.ListenAddr, cfg.IdleTimeout)
	}
}

func TestConfigFromEnvPrefersProxyAuthOverFile(t *testing.T) {
	setEnv(t, map[string]string{
		envProxyAuth:     "user:value",
		envProxyAuthFile: filepath.Join(t.TempDir(), "missing"),
		envProxyHost:     "172.16.0.11",
	})
	cfg, err := ConfigFromEnv()
	if err != nil || cfg.Credential != "user:value" {
		t.Fatalf("credential = %q, %v", cfg.Credential, err)
	}
}

func TestConfigFromEnvRefusals(t *testing.T) {
	good := map[string]string{envProxyAuth: "user:value", envProxyHost: "172.16.0.11"}
	with := func(k, v string) map[string]string {
		m := map[string]string{}
		for key, val := range good {
			m[key] = val
		}
		m[k] = v
		return m
	}
	for name, tc := range map[string]struct {
		env  map[string]string
		want error
	}{
		"no credential":       {map[string]string{envProxyHost: "h"}, ErrCredentialIncomplete},
		"incomplete":          {with(envProxyAuth, "no-colon"), ErrCredentialIncomplete},
		"no host":             {with(envProxyHost, ""), errProxyHostRequired},
		"bad proxy port":      {with(envProxyPort, "70000"), errProxyPortInvalid},
		"non-numeric port":    {with(envProxyPort, "abc"), errProxyPortInvalid},
		"bad listen port":     {with(envListenPort, "0x10"), errListenPortInvalid},
		"out-of-range listen": {with(envListenPort, "99999"), errListenPortInvalid},
		"bad idle":            {with(envIdleTimeout, "soon"), errIdleTimeoutInvalid},
		"zero idle":           {with(envIdleTimeout, "0s"), errIdleTimeoutInvalid},
	} {
		t.Run(name, func(t *testing.T) {
			setEnv(t, tc.env)
			if _, err := ConfigFromEnv(); !errors.Is(err, tc.want) {
				t.Fatalf("error = %v, want %v", err, tc.want)
			}
		})
	}
}

func TestNewRefusesIncompleteCredentialAndMissingHost(t *testing.T) {
	if _, err := New(&Config{ProxyHost: "127.0.0.1", Credential: "user:"}); !errors.Is(err, ErrCredentialIncomplete) {
		t.Fatalf("error = %v", err)
	}
	if _, err := New(&Config{Credential: "user:value"}); !errors.Is(err, errProxyHostRequired) {
		t.Fatalf("error = %v", err)
	}
}

func TestNewRefusesInvalidPortAndNegativeTimeouts(t *testing.T) {
	base := Config{ProxyHost: "127.0.0.1", Credential: "user:value"}
	for name, mutate := range map[string]func(*Config){
		"port":       func(c *Config) { c.ProxyPort = 70000 },
		"idle":       func(c *Config) { c.IdleTimeout = -time.Second },
		"dial":       func(c *Config) { c.DialTimeout = -time.Second },
		"first read": func(c *Config) { c.FirstRead = -time.Second },
		"header":     func(c *Config) { c.HeaderTimeout = -time.Second },
	} {
		t.Run(name, func(t *testing.T) {
			cfg := base
			mutate(&cfg)
			if _, err := New(&cfg); err == nil {
				t.Fatal("New accepted an invalid config")
			}
		})
	}
}
