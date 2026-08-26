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
	"slices"
	"strings"
	"time"
)

const (
	sudoProbeTimeout          = 2 * time.Second
	darwinService             = "system/com.tailscale.tailscaled"
	darwinCLIPath             = "/opt/homebrew/bin/tailscale"
	darwinDaemonPath          = "/opt/homebrew/bin/tailscaled"
	darwinNetworkSetupPath    = "/usr/sbin/networksetup"
	darwinQuad100             = "100.100.100.100"
	darwinNetworkSetupTimeout = 20 * time.Second
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

type darwinDNSService struct {
	Name    string   `json:"name"`
	Servers []string `json:"servers,omitempty"`
}

type darwinDNSSnapshot struct {
	Services []darwinDNSService `json:"services"`
}

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

func securePlatformTokenDir(context.Context) error {
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
	return darwinRuntimeRegistered(ctx)
}

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
func platformNetworkPrepare(ctx context.Context, _ string) error {
	if !platformRuntimeReady() {
		return fmt.Errorf("open-source macOS tailscaled DNS setup requires passwordless administrative access")
	}
	services, err := darwinNetworkServices(ctx)
	if err != nil {
		return err
	}
	snapshot := darwinDNSSnapshot{Services: make([]darwinDNSService, 0, len(services))}
	for _, service := range services {
		servers, getErr := darwinGetDNSServers(ctx, service)
		if getErr != nil {
			return getErr
		}
		snapshot.Services = append(snapshot.Services, darwinDNSService{Name: service, Servers: servers})
	}
	data, err := json.Marshal(snapshot)
	if err != nil {
		return fmt.Errorf("failed to encode macOS DNS snapshot: %w", err)
	}
	if err := writeFileAtomically(darwinDNSSnapshotPath(), data); err != nil {
		return fmt.Errorf("failed to persist macOS DNS snapshot: %w", err)
	}
	return nil
}

func platformNetworkActivate(ctx context.Context, _ string) error {
	snapshot, err := readDarwinDNSSnapshot()
	if err != nil {
		return err
	}
	for _, service := range snapshot.Services {
		if err := darwinSetDNSServers(ctx, service.Name, []string{darwinQuad100}); err != nil {
			return err
		}
		configured, getErr := darwinGetDNSServers(ctx, service.Name)
		if getErr != nil || !slices.Equal(configured, []string{darwinQuad100}) {
			return fmt.Errorf("failed to verify Quad100 DNS for macOS network service %q", service.Name)
		}
	}
	return nil
}

func platformNetworkRestore(ctx context.Context) error {
	if !platformNetworkResidue() {
		return nil
	}
	if !platformRuntimeReady() {
		return fmt.Errorf("open-source macOS tailscaled DNS cleanup requires passwordless administrative access")
	}
	snapshot, err := readDarwinDNSSnapshot()
	if err != nil {
		return err
	}
	for _, service := range snapshot.Services {
		if err := darwinSetDNSServers(ctx, service.Name, service.Servers); err != nil {
			return err
		}
		configured, getErr := darwinGetDNSServers(ctx, service.Name)
		if getErr != nil || !slices.Equal(configured, service.Servers) {
			return fmt.Errorf("failed to verify restored DNS for macOS network service %q", service.Name)
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
	if err := json.Unmarshal(data, &snapshot); err != nil || len(snapshot.Services) == 0 {
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
	if os.Geteuid() == 0 {
		return exec.CommandContext(ctx, path, args...)
	}
	privilegedArgs := make([]string, 0, len(args)+privilegedExtraArgs)
	privilegedArgs = append(privilegedArgs, "-n", path)
	privilegedArgs = append(privilegedArgs, args...)
	return exec.CommandContext(ctx, "/usr/bin/sudo", privilegedArgs...)
}

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
