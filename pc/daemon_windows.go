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
	"os/user"
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

func platformReady(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	_, err := tailscaleCommandContext(ctx, path, "status", "--json").CombinedOutput()
	return err == nil
}

func securePlatformTokenDir() error {
	currentUser, err := user.Current()
	if err != nil || currentUser.Username == "" {
		return fmt.Errorf("pc: failed to identify the Windows runtime user")
	}
	out, err := exec.Command(
		"icacls",
		TokenDir,
		"/inheritance:r",
		"/grant:r",
		currentUser.Username+":(OI)(CI)F",
		"*S-1-5-18:(OI)(CI)F",
	).CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: failed to secure state directory ACL: %w (output: %s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func legacyEgressResidue() bool {
	ctx, cancel := context.WithTimeout(context.Background(), legacyInspectionTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, "netsh", "advfirewall", "firewall", "show", "rule", "name=all").CombinedOutput()
	if err != nil || strings.Contains(string(out), "Egress-Allow-") {
		return true
	}
	policy, policyErr := exec.CommandContext(
		ctx, "netsh", "advfirewall", "show", "allprofiles").CombinedOutput()
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
