// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build windows

package pc

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

var (
	TokenDir  = filepath.Join(os.Getenv("ProgramData"), "Harness", "private-connectivity")
	TokenFile = filepath.Join(TokenDir, "oidc-token")
)

func tailscalePath() (string, error) {
	candidates := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Tailscale", "tailscale.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Tailscale", "tailscale.exe"),
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return exec.LookPath("tailscale.exe")
}

func securePlatformTokenDir(ctx context.Context) error {
	tokenUser, err := windows.GetCurrentProcessToken().GetTokenUser()
	if err != nil || tokenUser == nil || tokenUser.User.Sid == nil {
		return fmt.Errorf("pc: failed to identify the Windows runtime SID")
	}
	runtimeSID := tokenUser.User.Sid.String()
	args := []string{
		TokenDir,
		"/inheritance:r",
		"/grant:r",
		"*" + runtimeSID + ":(OI)(CI)F",
	}
	if runtimeSID != "S-1-5-18" {
		args = append(args, "*S-1-5-18:(OI)(CI)F")
	}
	commandCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	out, err := exec.CommandContext(commandCtx, "icacls", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: failed to secure state directory ACL: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeStart(ctx context.Context) error {
	command := "$service = Get-Service -Name 'Tailscale' -ErrorAction Stop; " +
		"if ($service.Status -ne 'Running') { Start-Service -Name 'Tailscale' -ErrorAction Stop }; " +
		"$service.WaitForStatus('Running', [TimeSpan]::FromSeconds(30)); " +
		"if ($service.Status -ne 'Running') { throw 'Tailscale service did not become running' }"
	// PowerShell owns the 30-second service wait. Keep the outer process deadline slightly larger
	// so WaitForStatus can return its useful failure instead of being killed at the same instant.
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceCommandTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to start Tailscale service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	command := "$service = Get-Service -Name 'Tailscale' -ErrorAction Stop; " +
		"if ($service.Status -ne 'Stopped') { Stop-Service -Name 'Tailscale' -Force -ErrorAction Stop }; " +
		"$service.WaitForStatus('Stopped', [TimeSpan]::FromSeconds(30)); " +
		"if ($service.Status -ne 'Stopped') { throw 'Tailscale service did not become stopped' }"
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceCommandTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop Tailscale service: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformNetworkPrepare(context.Context) error { return nil }

// Tailscale owns Windows host DNS. The tailnet's active global defaults make Quad100 a complete
// resolver for public, split-DNS, App Connector, and MagicDNS names, so Lite Engine must not add a
// second NRPT policy layer.
func platformNetworkActivate(context.Context) error { return nil }

func platformNetworkRestore(context.Context) error { return nil }

func platformNetworkResidue() bool { return false }
