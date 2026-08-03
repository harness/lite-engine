// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build linux

package pc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const linuxStateDir = "/var/lib/harness-pc"

var (
	TokenDir   = linuxStateDir
	TokenFile  = filepath.Join(TokenDir, "oidc-token")
	MarkerFile = filepath.Join(TokenDir, "lifecycle")
)

func tailscalePath() (string, error) {
	return exec.LookPath("tailscale")
}

func securePlatformTokenDir() error {
	return nil
}

func legacyEgressResidue(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, legacyInspectionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "iptables", "-S", "OUTPUT").CombinedOutput()
	if err != nil {
		return true
	}
	for _, rule := range strings.Split(string(out), "\n") {
		switch strings.Join(strings.Fields(rule), " ") {
		case "-A OUTPUT -o lo -j ACCEPT",
			"-A OUTPUT -m state --state ESTABLISHED,RELATED -j ACCEPT",
			"-A OUTPUT -m state --state RELATED,ESTABLISHED -j ACCEPT",
			"-A OUTPUT -p udp --dport 53 -j ACCEPT",
			"-A OUTPUT -p udp -m udp --dport 53 -j ACCEPT",
			"-A OUTPUT -p tcp --dport 53 -j ACCEPT",
			"-A OUTPUT -p tcp -m tcp --dport 53 -j ACCEPT",
			"-A OUTPUT -j DROP":
			return true
		}
	}
	return false
}

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
