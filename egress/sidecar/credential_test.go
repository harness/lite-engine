package sidecar

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseCredentialRefusesIncomplete(t *testing.T) {
	cases := map[string]string{
		"missing colon": "poc",
		"empty user":    ":pass",
		"empty pass":    "user:",
		"empty":         "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if _, err := ParseCredential(raw); !errors.Is(err, ErrCredentialIncomplete) {
				t.Fatalf("ParseCredential(%q) error = %v, want ErrCredentialIncomplete", raw, err)
			}
		})
	}
}

func TestParseCredentialAcceptsUserAndPassword(t *testing.T) {
	got, err := ParseCredential("poc:pocpass")
	if err != nil || got != "poc:pocpass" {
		t.Fatalf("ParseCredential = %q, %v; want poc:pocpass, nil", got, err)
	}
}

func TestReadCredentialFileTrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("  acct|org:token\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ReadCredentialFile(path)
	if err != nil || got != "acct|org:token" {
		t.Fatalf("ReadCredentialFile = %q, %v; want acct|org:token, nil", got, err)
	}
}

func TestReadCredentialFileRefusesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadCredentialFile(path); !errors.Is(err, ErrCredentialIncomplete) {
		t.Fatalf("error = %v, want ErrCredentialIncomplete", err)
	}
}

func TestBasicAuthEncodesCredential(t *testing.T) {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("poc:pocpass"))
	if got := BasicAuth("poc:pocpass"); got != want {
		t.Fatalf("BasicAuth = %q, want %q", got, want)
	}
}
