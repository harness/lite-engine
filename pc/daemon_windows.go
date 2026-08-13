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
	TokenDir   = filepath.Join(os.Getenv("ProgramData"), "Harness", "private-connectivity")
	TokenFile  = filepath.Join(TokenDir, "oidc-token")
	MarkerFile = filepath.Join(TokenDir, "lifecycle")
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

func securePlatformTokenDir() error {
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
	out, err := exec.Command("icacls", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: failed to secure state directory ACL: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeReady() bool {
	ctx, cancel := context.WithTimeout(context.Background(), runtimeStatusTimeout)
	defer cancel()
	return exec.CommandContext(ctx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command",
		"Get-Service -Name 'Tailscale' -ErrorAction Stop | Out-Null").Run() == nil
}

func platformRuntimeStart(ctx context.Context) error {
	command := "$service = Get-Service -Name 'Tailscale' -ErrorAction Stop; " +
		"if ($service.Status -ne 'Running') { Start-Service -Name 'Tailscale' -ErrorAction Stop }; " +
		"$service.WaitForStatus('Running', [TimeSpan]::FromSeconds(30)); " +
		"if ($service.Status -ne 'Running') { throw 'Tailscale service did not become running' }"
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
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
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", command).CombinedOutput(); err != nil {
		return fmt.Errorf("failed to stop Tailscale service: %w (%s)", err, strings.TrimSpace(string(out)))
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
	ruleCtx, cancelRules := context.WithTimeout(ctx, legacyInspectionTimeout)
	ruleCommand := "$rule = Get-NetFirewallRule -DisplayName 'Egress-Allow-*' " +
		"-ErrorAction SilentlyContinue | Select-Object -First 1; " +
		"if ($null -ne $rule) { Write-Output 'present' }"
	out, err := exec.CommandContext(
		ruleCtx, "powershell.exe", "-NoProfile", "-NonInteractive", "-Command", ruleCommand).CombinedOutput()
	cancelRules()
	if err != nil || strings.TrimSpace(string(out)) == "present" {
		return true
	}

	policyCtx, cancelPolicy := context.WithTimeout(ctx, legacyInspectionTimeout)
	policy, policyErr := exec.CommandContext(
		policyCtx, "netsh", "advfirewall", "show", "allprofiles").CombinedOutput()
	cancelPolicy()
	return policyErr != nil || strings.Contains(strings.ToLower(string(policy)), "blockoutbound")
}

func replaceFileAtomically(source, destination string) error {
	sourcePtr, err := windows.UTF16PtrFromString(source)
	if err != nil {
		return err
	}
	destinationPtr, err := windows.UTF16PtrFromString(destination)
	if err != nil {
		return err
	}
	return windows.MoveFileEx(
		sourcePtr,
		destinationPtr,
		windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH,
	)
}
