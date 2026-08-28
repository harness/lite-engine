// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build linux

package pc

import (
	"context"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
)

const linuxStateDir = "/var/lib/harness-pc"

var (
	TokenDir  = linuxStateDir
	TokenFile = filepath.Join(TokenDir, "oidc-token")
)

func tailscalePath() (string, error) {
	return exec.LookPath("tailscale")
}

func securePlatformTokenDir(context.Context) error { return nil }

func platformRuntimeStart(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	out, err := exec.CommandContext(commandCtx, "systemctl", "start", "tailscaled.service").CombinedOutput()
	cancel()
	if err != nil {
		return fmt.Errorf("systemctl start tailscaled failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformRuntimeStop(ctx context.Context) error {
	commandCtx, cancel := context.WithTimeout(ctx, runtimeServiceTimeout)
	defer cancel()
	if out, err := exec.CommandContext(commandCtx, "systemctl", "stop", "tailscaled.service").CombinedOutput(); err != nil {
		return fmt.Errorf("systemctl stop tailscaled failed: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func platformNetworkPrepare(context.Context) error  { return nil }
func platformNetworkActivate(context.Context) error { return nil }
func platformNetworkRestore(context.Context) error  { return nil }
func platformNetworkResidue() bool                  { return false }
