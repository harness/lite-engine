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

const sudoProbeTimeout = 2 * time.Second

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
	if cliErr == nil && daemonErr == nil &&
		!cliInfo.IsDir() && cliInfo.Mode()&0111 != 0 &&
		!daemonInfo.IsDir() && daemonInfo.Mode()&0111 != 0 {
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
	ctx, cancel := context.WithTimeout(context.Background(), sudoProbeTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "/usr/bin/sudo", "-n", "true").Run() == nil
}

func platformRuntimeRunning(ctx context.Context) (running, known bool) {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, "/bin/launchctl", "print",
		"system/com.tailscale.tailscaled").CombinedOutput()
	if err == nil {
		return true, true
	}
	message := strings.ToLower(string(out))
	if strings.Contains(message, "could not find service") ||
		strings.Contains(message, "service could not be found") {
		return false, true
	}
	return false, false
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

// Tailscale must own macOS host DNS; Lite Engine does not install a second DNS policy layer.
// Open-source tailscaled DNS behavior has varied by release, so the hosted image must prove native
// public, MagicDNS, split-DNS, and App Connector resolution during acceptance testing.
func platformNetworkPrepare(context.Context, string) error { return nil }

func platformNetworkActivate(context.Context, string) error { return nil }

func platformNetworkRestore(context.Context) error { return nil }

func platformNetworkResidue() bool { return false }

func darwinPrivilegedCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, path, args...)
	}
	privilegedArgs := make([]string, 0, len(args)+privilegedExtraArgs)
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
