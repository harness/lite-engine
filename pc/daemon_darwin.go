// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build darwin

package pc

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

const (
	darwinService             = "system/com.tailscale.tailscaled"
	darwinCLIPath             = "/opt/homebrew/bin/tailscale"
	darwinDaemonPath          = "/opt/homebrew/bin/tailscaled"
	darwinNetworkSetupPath    = "/usr/sbin/networksetup"
	darwinEnvPath             = "/usr/bin/env"
	darwinQuad100             = "100.100.100.100"
	darwinNetworkSetupTimeout = 20 * time.Second
)

var (
	TokenDir  = darwinStateDirectory()
	TokenFile = filepath.Join(TokenDir, "oidc-token")
)

func darwinStateDirectory() string {
	home, err := os.UserHomeDir()
	if err == nil && strings.TrimSpace(home) != "" {
		return filepath.Join(home, "Library", "Application Support", "Harness", "private-connectivity")
	}
	// Keep state on a privileged durable path when the hosted user's home is unavailable.
	return "/Library/Application Support/Harness/private-connectivity"
}

type darwinDNSSnapshot map[string][]string

func darwinDNSSnapshotPath() string {
	return filepath.Join(TokenDir, "darwin-dns.json")
}

func tailscalePath() (string, error) {
	cliInfo, cliErr := os.Stat(darwinCLIPath)
	daemonInfo, daemonErr := os.Stat(darwinDaemonPath)
	if cliErr == nil && daemonErr == nil &&
		!cliInfo.IsDir() && cliInfo.Mode()&0111 != 0 &&
		!daemonInfo.IsDir() && daemonInfo.Mode()&0111 != 0 {
		return darwinCLIPath, nil
	}
	return "", fmt.Errorf("pc: installed open-source macOS tailscale runtime is unavailable")
}

func securePlatformTokenDir(context.Context) error { return nil }

func platformRuntimeStart(ctx context.Context) error {
	registered, known := darwinRuntimeRegistered(ctx)
	if !known {
		return fmt.Errorf("tailscaled launchd state could not be inspected")
	}
	if registered {
		return nil
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, darwinDaemonPath, "install-system-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to register preinstalled tailscaled with launchd: %w (%s)",
			err, strings.TrimSpace(string(out)))
	}
	if registered, known := darwinRuntimeRegistered(ctx); !known || !registered {
		return fmt.Errorf("tailscaled launchd service did not become available")
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
	out, err := darwinPrivilegedCommand(commandCtx, darwinDaemonPath, "uninstall-system-daemon").CombinedOutput()
	if err != nil {
		return fmt.Errorf("failed to unregister preinstalled tailscaled from launchd: %w (%s)",
			err, strings.TrimSpace(string(out)))
	}
	if registered, known := darwinRuntimeRegistered(ctx); !known || registered {
		return fmt.Errorf("tailscaled launchd service remains registered after cleanup")
	}
	return nil
}

func darwinRuntimeRegistered(ctx context.Context) (registered, known bool) {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, "/bin/launchctl", "print", darwinService).CombinedOutput()
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

// The open-source headless macOS daemon does not configure system DNS. Capture the exact existing
// resolver state before joining, then use its device-local Quad100 resolver for the stage. The
// tailnet supplies public fallback resolvers as well as split DNS, App Connector, and MagicDNS.
func platformNetworkPrepare(ctx context.Context) error {
	services, err := darwinNetworkServices(ctx)
	if err != nil {
		return err
	}
	snapshot := make(darwinDNSSnapshot, len(services))
	for _, service := range services {
		servers, getErr := darwinGetDNSServers(ctx, service)
		if getErr != nil {
			return getErr
		}
		snapshot[service] = servers
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to encode macOS DNS snapshot: %w", err)
	}
	if err := writePrivateFile(darwinDNSSnapshotPath(), data); err != nil {
		return fmt.Errorf("failed to persist macOS DNS snapshot: %w", err)
	}
	return nil
}

func platformNetworkActivate(ctx context.Context) error {
	snapshot, err := readDarwinDNSSnapshot()
	if err != nil {
		return err
	}
	for service := range snapshot {
		if err := darwinSetDNSServers(ctx, service, []string{darwinQuad100}); err != nil {
			return err
		}
	}
	return nil
}

func platformNetworkRestore(ctx context.Context) error {
	if !platformNetworkResidue() {
		return nil
	}
	snapshot, err := readDarwinDNSSnapshot()
	if err != nil {
		return err
	}
	for service, servers := range snapshot {
		if err := darwinSetDNSServers(ctx, service, servers); err != nil {
			return err
		}
	}
	return removeFile(darwinDNSSnapshotPath())
}

func platformNetworkResidue() bool {
	return fileExists(darwinDNSSnapshotPath())
}

func readDarwinDNSSnapshot() (darwinDNSSnapshot, error) {
	var snapshot darwinDNSSnapshot
	data, err := os.ReadFile(darwinDNSSnapshotPath())
	if err != nil {
		return snapshot, fmt.Errorf("failed to read macOS DNS snapshot: %w", err)
	}
	if err := json.Unmarshal(data, &snapshot); err != nil || len(snapshot) == 0 {
		return snapshot, fmt.Errorf("macOS DNS snapshot is invalid")
	}
	return snapshot, nil
}

func darwinNetworkServices(ctx context.Context) ([]string, error) {
	out, err := darwinNetworkSetup(ctx, "-listallnetworkservices")
	if err != nil {
		return nil, err
	}
	services := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		service := strings.TrimSpace(line)
		if service == "" || strings.HasPrefix(service, "An asterisk") || strings.HasPrefix(service, "*") {
			continue
		}
		services = append(services, service)
	}
	if len(services) == 0 {
		return nil, fmt.Errorf("no enabled macOS network service is available")
	}
	return services, nil
}

func darwinGetDNSServers(ctx context.Context, service string) ([]string, error) {
	out, err := darwinNetworkSetup(ctx, "-getdnsservers", service)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(out, "There aren't any DNS Servers set on") {
		return nil, nil
	}
	servers := make([]string, 0)
	for _, line := range strings.Split(out, "\n") {
		server := strings.TrimSpace(line)
		if server != "" {
			servers = append(servers, server)
		}
	}
	return servers, nil
}

func darwinSetDNSServers(ctx context.Context, service string, servers []string) error {
	args := []string{"-setdnsservers", service}
	if len(servers) == 0 {
		args = append(args, "empty")
	} else {
		args = append(args, servers...)
	}
	_, err := darwinNetworkSetup(ctx, args...)
	return err
}

func darwinNetworkSetup(ctx context.Context, args ...string) (string, error) {
	commandCtx, cancel := context.WithTimeout(ctx, darwinNetworkSetupTimeout)
	defer cancel()
	out, err := darwinPrivilegedCommand(commandCtx, darwinNetworkSetupPath, args...).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("networksetup %s failed: %w (%s)", args[0], err, strings.TrimSpace(string(out)))
	}
	return strings.TrimSpace(string(out)), nil
}

func darwinPrivilegedCommand(ctx context.Context, path string, args ...string) *exec.Cmd {
	// networksetup and launchctl emit text that we must classify. Force a stable locale on the
	// privileged side of sudo so host language settings cannot change those protocol strings.
	commandArgs := append([]string{"LC_ALL=C", "LANG=C", path}, args...)
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, darwinEnvPath, commandArgs...)
	}
	privilegedArgs := append([]string{"-n", darwinEnvPath}, commandArgs...)
	return exec.CommandContext(ctx, "/usr/bin/sudo", privilegedArgs...)
}
