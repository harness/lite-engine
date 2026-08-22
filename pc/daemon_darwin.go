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

const (
	sudoProbeTimeout   = 2 * time.Second
	darwinService      = "system/com.tailscale.tailscaled"
	darwinServicePlist = "/Library/LaunchDaemons/com.tailscale.tailscaled.plist"
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
	info, err := os.Lstat(darwinServicePlist)
	if err != nil || !info.Mode().IsRegular() {
		return false
	}
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
		darwinService).CombinedOutput()
	if err == nil {
		return strings.Contains(strings.ToLower(string(out)), "state = running"), true
	}
	message := strings.ToLower(string(out))
	if strings.Contains(message, "could not find service") ||
		strings.Contains(message, "service could not be found") {
		return false, true
	}
	return false, false
}

func platformRuntimeStart(ctx context.Context) error {
	running, known := platformRuntimeRunning(ctx)
	if !known {
		return fmt.Errorf("tailscaled launchd state could not be inspected")
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	registered, registrationKnown := darwinRuntimeRegistered(commandCtx)
	if !registrationKnown {
		return fmt.Errorf("tailscaled launchd registration could not be inspected")
	}
	if !registered {
		out, err := darwinPrivilegedCommand(
			commandCtx, "/bin/launchctl", "bootstrap", "system", darwinServicePlist).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to bootstrap preinstalled tailscaled launchd service: %w (%s)",
				err, strings.TrimSpace(string(out)))
		}
	}
	if !running {
		out, err := darwinPrivilegedCommand(
			commandCtx, "/bin/launchctl", "kickstart", "-k", darwinService).CombinedOutput()
		if err != nil {
			return fmt.Errorf("failed to start preinstalled tailscaled launchd service: %w (%s)",
				err, strings.TrimSpace(string(out)))
		}
	}
	if running, known := platformRuntimeRunning(commandCtx); !known || !running {
		return fmt.Errorf("tailscaled launchd service did not become active")
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	registered, known := darwinRuntimeRegistered(ctx)
	if !known {
		return fmt.Errorf("tailscaled launchd registration could not be inspected")
	}
	if !registered {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, "/bin/launchctl", "bootout", darwinService).CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to stop preinstalled tailscaled launchd service: %w (%s)",
			err, strings.TrimSpace(string(out)))
	}
	if registered, known := darwinRuntimeRegistered(commandCtx); !known || registered {
		return fmt.Errorf("tailscaled launchd service remains loaded after cleanup")
	}
	return nil
}

func darwinRuntimeRegistered(ctx context.Context) (registered, known bool) {
	out, err := darwinPrivilegedCommand(ctx, "/bin/launchctl", "print", darwinService).CombinedOutput()
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
