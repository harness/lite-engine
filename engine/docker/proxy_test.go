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
