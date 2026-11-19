// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package sidecar

import (
	"encoding/base64"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestValidateCredentialRefusesIncomplete(t *testing.T) {
	cases := map[string]string{
		"missing colon": "justausername",
		"empty user":    ":value",
		"empty pass":    "user:",
		"empty":         "",
	}
	for name, raw := range cases {
		t.Run(name, func(t *testing.T) {
			if err := validateCredential(raw); !errors.Is(err, ErrCredentialIncomplete) {
				t.Fatalf("validateCredential error = %v, want ErrCredentialIncomplete", err)
			}
		})
	}
}

func TestValidateCredentialSplitsOnFirstColon(t *testing.T) {
	if err := validateCredential("acct|org:part1:part2"); err != nil {
		t.Fatalf("validateCredential = %v", err)
	}
}

func TestReadCredentialFileTrimsWhitespace(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("  acct|org:value\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := readCredentialFile(path)
	if err != nil || got != "acct|org:value" {
		t.Fatalf("readCredentialFile = %q, %v", got, err)
	}
}

func TestReadCredentialFileRefusesEmpty(t *testing.T) {
	path := filepath.Join(t.TempDir(), "proxy-auth")
	if err := os.WriteFile(path, []byte("\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := readCredentialFile(path); !errors.Is(err, ErrCredentialIncomplete) {
		t.Fatalf("error = %v, want ErrCredentialIncomplete", err)
	}
}

func TestBasicAuthEncodesCredential(t *testing.T) {
	want := "Basic " + base64.StdEncoding.EncodeToString([]byte("user:value"))
	if got := basicAuth("user:value"); got != want {
		t.Fatalf("basicAuth = %q, want %q", got, want)
	}
}
