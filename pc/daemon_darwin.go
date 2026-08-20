// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build darwin

package pc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

var (
	TokenDir   = darwinStateDirectory()
	TokenFile  = filepath.Join(TokenDir, "oidc-token")
	MarkerFile = filepath.Join(TokenDir, "lifecycle")
)

func darwinStateDirectory() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Library", "Application Support", "Harness", "private-connectivity")
	}
	// A missing home directory is an invalid hosted runtime. Keep the fallback on a
	// privileged durable path so a non-root process fails closed instead of placing
	// lifecycle evidence in an OS-cleanable temporary directory.
	return "/Library/Application Support/Harness/private-connectivity"
}

func tailscalePath() (string, error) {
	const cliPath = "/opt/homebrew/bin/tailscale"
	const daemonPath = "/opt/homebrew/bin/tailscaled"
	cliInfo, cliErr := os.Stat(cliPath)
	daemonInfo, daemonErr := os.Stat(daemonPath)
	if cliErr == nil && daemonErr == nil && !cliInfo.IsDir() && !daemonInfo.IsDir() {
		return cliPath, nil
	}
	return "", fmt.Errorf("pc: installed open-source macOS tailscale runtime is unavailable")
}

func securePlatformTokenDir() error {
	return nil
}

func platformRuntimeReady() bool {
	if os.Geteuid() == 0 {
		return true
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "true").Run() == nil
}

func platformRuntimeStart(ctx context.Context) error {
	const daemonPath = "/opt/homebrew/bin/tailscaled"
	if darwinRuntimeRegistered(ctx) {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, daemonPath, "install-system-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to register tailscaled with launchd: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if !darwinRuntimeRegistered(commandCtx) {
		return fmt.Errorf("tailscaled launchd service did not become available")
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	const daemonPath = "/opt/homebrew/bin/tailscaled"
	if !darwinRuntimeRegistered(ctx) {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, daemonPath, "uninstall-system-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to unregister tailscaled from launchd: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if darwinRuntimeRegistered(commandCtx) {
		return fmt.Errorf("tailscaled launchd service remains registered after cleanup")
	}
	return nil
}

func darwinRuntimeRegistered(ctx context.Context) bool {
	return darwinPrivilegedCommand(ctx, "/bin/launchctl", "print", "system/com.tailscale.tailscaled").Run() == nil
}

// The open-source macOS daemon owns DNS configuration. With the tailnet's active global DNS
// defaults, it installs Quad100 through SystemConfiguration and routes public, MagicDNS, split-DNS,
// and App Connector queries internally. Logout/service shutdown removes that configuration; Lite
// Engine must not install a second DNS policy layer.
func platformNetworkPrepare(context.Context, string) error { return nil }

func platformNetworkActivate(context.Context, string) error { return nil }

func platformNetworkRestore(context.Context) error { return nil }

func platformNetworkResidue() bool { return false }

func darwinPrivilegedCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, path, args...)
	}
	privilegedArgs := make([]string, 0, len(args)+2)
	privilegedArgs = append(privilegedArgs, "-n", path)
	privilegedArgs = append(privilegedArgs, args...)
	return exec.CommandContext(ctx, "/usr/bin/sudo", privilegedArgs...)
}

func legacyEgressResidue(context.Context) bool {
	return false
}

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
