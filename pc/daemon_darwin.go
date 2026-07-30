// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

//go:build darwin

package pc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
)

var (
	TokenDir   = darwinStateDir()
	TokenFile  = filepath.Join(TokenDir, "oidc-token")
	MarkerFile = filepath.Join(TokenDir, "lifecycle")
)

func darwinStateDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return filepath.Join(os.TempDir(), "harness-private-connectivity")
	}
	return filepath.Join(home, "Library", "Application Support", "Harness", "private-connectivity")
}

func tailscalePath() (string, error) {
	for _, candidate := range []string{
		"/Applications/Tailscale.app/Contents/MacOS/Tailscale",
		"/usr/local/bin/tailscale",
		"/opt/homebrew/bin/tailscale",
	} {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	return exec.LookPath("tailscale")
}

func platformReady(path string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
	defer cancel()
	_, err := tailscaleCommandContext(ctx, path, "status", "--json").CombinedOutput()
	return err == nil
}

func securePlatformTokenDir() error {
	return nil
}

func legacyEgressResidue() bool {
	return false
}

func replaceFileAtomically(source, destination string) error {
	return os.Rename(source, destination)
}
