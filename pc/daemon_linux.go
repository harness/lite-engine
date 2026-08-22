// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build linux

package pc

import (
	"context"
	"fmt"
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

func platformRuntimeReady() bool {
	if _, err := exec.LookPath("tailscaled"); err != nil {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "systemctl", "cat", "tailscaled.service").Run() == nil
}

func platformRuntimeRunning(ctx context.Context) (running, known bool) {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, "systemctl", "show", "tailscaled.service",
		"--property=ActiveState", "--value").CombinedOutput()
	if err != nil {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(string(out))) {
	case "active", "activating", "reloading", "deactivating":
		return true, true
	case "inactive":
		return false, true
	default:
		return false, false
	}
}

func platformRuntimeStart(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "systemctl", "start", "tailscaled.service").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl start tailscaled failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if err := exec.CommandContext(commandCtx, "systemctl", "is-active", "--quiet", "tailscaled.service").Run(); err != nil {
		return fmt.Errorf("tailscaled did not become active: %w", err)
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "systemctl", "stop", "tailscaled.service").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl stop tailscaled failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	running, known := platformRuntimeRunning(ctx)
	if !known {
		return fmt.Errorf("tailscaled stopped but its inactive state could not be confirmed")
	}
	if running {
		return fmt.Errorf("tailscaled remains active after stop")
	}
	return nil
}

func platformNetworkPrepare(context.Context, string) error {
	return nil
}

func platformNetworkActivate(context.Context, string) error {
	return nil
}

func platformNetworkRestore(context.Context) error {
	return nil
}

func platformNetworkResidue() bool {
	return false
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
