// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package docker

import (
	"testing"
)

func TestBuildCredentialedProxyURL(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
		proxyURL string
		want     string
		wantErr  bool
	}{
		{
			name:     "no credentials returns URL unchanged",
			username: "",
			password: "",
			proxyURL: "http://proxy.example.com:3128",
			want:     "http://proxy.example.com:3128",
		},
		{
			name:     "username only returns URL unchanged",
			username: "user",
			password: "",
			proxyURL: "http://proxy.example.com:3128",
			want:     "http://proxy.example.com:3128",
		},
		{
			name:     "password only returns URL unchanged",
			username: "",
			password: "pass",
			proxyURL: "http://proxy.example.com:3128",
			want:     "http://proxy.example.com:3128",
		},
		{
			name:     "plain credentials embedded",
			username: "alice",
			password: "secret",
			proxyURL: "http://proxy.example.com:3128",
			want:     "http://alice:secret@proxy.example.com:3128",
		},
		{
			name:     "special chars in password are percent-encoded",
			username: "acct123",
			password: "p@ss:w0rd$",
			proxyURL: "http://proxy.example.com:3128",
			want:     "http://acct123:p%40ss%3Aw0rd$@proxy.example.com:3128",
		},
		{
			name:     "bare URL without scheme gets http:// prefix",
			username: "user",
			password: "pass",
			proxyURL: "proxy.example.com:3128",
			want:     "http://user:pass@proxy.example.com:3128",
		},
		{
			name:     "invalid URL returns error",
			username: "user",
			password: "pass",
			proxyURL: "://bad url",
			wantErr:  true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := buildCredentialedProxyURL(tc.username, tc.password, tc.proxyURL)
			if (err != nil) != tc.wantErr {
				t.Fatalf("buildCredentialedProxyURL() error = %v, wantErr %v", err, tc.wantErr)
			}
			if !tc.wantErr && got != tc.want {
				t.Errorf("buildCredentialedProxyURL() = %q, want %q", got, tc.want)
			}
		})
	}
}

func TestBuildSystemdProxyConf(t *testing.T) {
	tests := []struct {
		name     string
		proxyURL string
		noProxy  string
		want     string
	}{
		{
			name:     "plain URL without percent-encoding is unchanged",
			proxyURL: "http://user:pass@proxy.example.com:3128",
			noProxy:  "localhost,127.0.0.1",
			want: `[Service]
Environment="HTTP_PROXY=http://user:pass@proxy.example.com:3128"
Environment="HTTPS_PROXY=http://user:pass@proxy.example.com:3128"
Environment="NO_PROXY=localhost,127.0.0.1"
`,
		},
		{
			name:     "percent-encoded chars are escaped for systemd specifiers",
			proxyURL: "http://token%7Corg%7Cuser:pass%40word@172.22.64.9:3128",
			noProxy:  "localhost,127.0.0.1",
			want: `[Service]
Environment="HTTP_PROXY=http://token%%7Corg%%7Cuser:pass%%40word@172.22.64.9:3128"
Environment="HTTPS_PROXY=http://token%%7Corg%%7Cuser:pass%%40word@172.22.64.9:3128"
Environment="NO_PROXY=localhost,127.0.0.1"
`,
		},
		{
			name:     "percent signs in noProxy are also escaped",
			proxyURL: "http://proxy.example.com:3128",
			noProxy:  "localhost,%25.internal",
			want: `[Service]
Environment="HTTP_PROXY=http://proxy.example.com:3128"
Environment="HTTPS_PROXY=http://proxy.example.com:3128"
Environment="NO_PROXY=localhost,%%25.internal"
`,
		},
		{
			name:     "empty values render empty env vars",
			proxyURL: "",
			noProxy:  "",
			want: `[Service]
Environment="HTTP_PROXY="
Environment="HTTPS_PROXY="
Environment="NO_PROXY="
`,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildSystemdProxyConf(tc.proxyURL, tc.noProxy); got != tc.want {
				t.Errorf("buildSystemdProxyConf() = %q, want %q", got, tc.want)
			}
		})
	}
}
