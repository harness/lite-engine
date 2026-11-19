package sidecar

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestConfigFromEnvReadsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("acct|org:token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PROXY_AUTH", "")
	t.Setenv("PROXY_AUTH_FILE", path)
	t.Setenv("UPSTREAM_PROXY_HOST", "172.16.0.11")
	t.Setenv("UPSTREAM_PROXY_PORT", "")
	t.Setenv("LISTEN_ADDR", "")
	t.Setenv("LISTEN_PORT", "")
	t.Setenv("IDLE_TIMEOUT", "90s")

	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Credential != "acct|org:token" || cfg.ProxyHost != "172.16.0.11" || cfg.ProxyPort != 3128 {
		t.Fatalf("config = %+v", cfg)
	}
	if cfg.IdleTimeout != 90*time.Second {
		t.Fatalf("idle = %s", cfg.IdleTimeout)
	}
}

func TestConfigFromEnvPrefersProxyAuth(t *testing.T) {
	t.Setenv("PROXY_AUTH", "user:pass")
	t.Setenv("PROXY_AUTH_FILE", filepath.Join(t.TempDir(), "missing"))
	t.Setenv("UPSTREAM_PROXY_HOST", "172.16.0.11")
	t.Setenv("IDLE_TIMEOUT", "")
	cfg, err := ConfigFromEnv()
	if err != nil {
		t.Fatal(err)
	}
	if cfg.Credential != "user:pass" {
		t.Fatalf("credential = %q", cfg.Credential)
	}
}

func TestConfigFromEnvRefusesIncompleteCredential(t *testing.T) {
	t.Setenv("PROXY_AUTH", "no-colon")
	t.Setenv("UPSTREAM_PROXY_HOST", "172.16.0.11")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestConfigFromEnvRequiresHost(t *testing.T) {
	t.Setenv("PROXY_AUTH", "user:pass")
	t.Setenv("UPSTREAM_PROXY_HOST", "")
	if _, err := ConfigFromEnv(); err == nil {
		t.Fatal("expected an error")
	}
}

func TestNewRefusesIncompleteCredential(t *testing.T) {
	_, err := New(Config{ProxyHost: "127.0.0.1", Credential: "user:"})
	if err == nil {
		t.Fatal("expected an error")
	}
}
