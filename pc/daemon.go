// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	logoutTimeout                = 30 * time.Second
	loggedOutWaitTimeout         = 10 * time.Second
	loggedOutProbeTimeout        = 2 * time.Second
	runtimeStatusTimeout         = 8 * time.Second
	runtimeServiceTimeout        = 30 * time.Second
	runtimeServiceCommandTimeout = runtimeServiceTimeout + 5*time.Second
	joinCommandTimeout           = joinTimeout + 5*time.Second
	loggedOutPollInterval        = 200 * time.Millisecond
	privateFileMode              = os.FileMode(0600)
	privateDirectoryMode         = os.FileMode(0700)
)

var errUnsupported = errors.New("pc: an installed tailscale runtime is required")

// JoinAndConfigure joins the host to the customer network. Host routes are owned by tailscaled.
// Host DNS is owned by Tailscale except for the headless macOS runtime, whose Quad100 resolver is
// activated and restored by the platform hooks. Container DNS is applied by engine setup.
func JoinAndConfigure(ctx context.Context, cfg *Config) error { //nolint:gocyclo,funlen // Ordered fail-closed lifecycle.
	path, err := tailscalePath()
	if err != nil {
		return errUnsupported
	}
	if fileExists(TokenFile) || platformNetworkResidue() {
		return fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden")
	}
	cleanupCtx := context.WithoutCancel(ctx)
	if err := platformRuntimeStart(ctx); err != nil {
		return errors.Join(
			fmt.Errorf("pc: failed to start tailscaled service: %w", err),
			platformRuntimeStop(cleanupCtx),
		)
	}
	if waitErr := waitForLoggedOut(ctx, path); waitErr != nil {
		stopErr := platformRuntimeStop(cleanupCtx)
		return errors.Join(
			fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden: %w", waitErr),
			stopErr,
		)
	}
	if err := secureTokenDir(ctx); err != nil {
		return errors.Join(err, platformRuntimeStop(cleanupCtx))
	}
	if err := platformNetworkPrepare(ctx); err != nil {
		return errors.Join(
			fmt.Errorf("pc: failed to prepare platform networking: %w", err),
			platformRuntimeStop(cleanupCtx),
		)
	}
	if err := writePrivateFile(TokenFile, []byte(cfg.OIDCToken)); err != nil {
		return rollbackSetup(ctx, fmt.Errorf("pc: failed to write OIDC token file: %w", err))
	}

	// The CLI owns joinTimeout. Give the process a small outer grace period so its own timeout and
	// diagnostics can complete before Go forcefully terminates it.
	joinCtx, cancel := context.WithTimeout(ctx, joinCommandTimeout)
	defer cancel()
	args := []string{
		"up",
		"--client-id=" + wifClientID(cfg.ClientID),
		"--id-token=file:" + TokenFile,
		"--advertise-tags=" + cfg.Tag,
		"--accept-routes",
		"--accept-dns=true",
		"--hostname=" + cfg.Hostname,
		fmt.Sprintf("--timeout=%ds", int(joinTimeout.Seconds())),
	}
	// Windows otherwise associates the session with an interactive user. Hosted VMs run headless,
	// so keep the system service connected for the full stage lifetime.
	if runtime.GOOS == "windows" {
		args = append(args, "--unattended=true")
	}
	out, joinErr := tailscaleCommandContext(joinCtx, path, args...).CombinedOutput()
	if joinErr != nil {
		safeOutput := strings.ReplaceAll(string(out), cfg.OIDCToken, "[REDACTED]")
		return rollbackSetup(ctx, fmt.Errorf("pc: tailscale up failed: %w (%s)", joinErr, strings.TrimSpace(safeOutput)))
	}
	if err := platformNetworkActivate(ctx); err != nil {
		return rollbackSetup(ctx, fmt.Errorf("pc: failed to activate platform networking: %w", err))
	}
	if err := removeTokenFile(); err != nil {
		return rollbackSetup(ctx, err)
	}
	logrus.WithField("hostname", cfg.Hostname).Infoln("pc: joined customer network")
	return nil
}

func rollbackSetup(ctx context.Context, cause error) error {
	if cleanupErr := Logout(ctx); cleanupErr != nil {
		return errors.Join(cause, fmt.Errorf("pc: setup rollback failed: %w", cleanupErr))
	}
	return cause
}

// Logout removes the authenticated session and stops the OS-managed daemon before VM reuse.
func Logout(ctx context.Context) error {
	// Cleanup must outlive the HTTP request that initiated it. Each platform command below
	// retains its own deadline, so detaching cancellation cannot make cleanup unbounded.
	ctx = context.WithoutCancel(ctx)
	path, pathErr := tailscalePath()
	var cleanupErr error
	if pathErr != nil {
		cleanupErr = errUnsupported
	} else if startErr := platformRuntimeStart(ctx); startErr != nil {
		cleanupErr = fmt.Errorf("pc: failed to start tailscaled for cleanup: %w", startErr)
	} else {
		logoutCtx, cancel := context.WithTimeout(ctx, logoutTimeout)
		logoutErr := tailscaleCommandContext(logoutCtx, path, "logout").Run()
		cancel()
		if logoutErr != nil {
			cleanupErr = fmt.Errorf("pc: tailscale logout failed: %w", logoutErr)
		}
	}
	// Restore platform-owned state even if logout failed.
	if restoreErr := platformNetworkRestore(ctx); restoreErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("pc: failed to restore platform networking: %w", restoreErr))
	}
	if stopErr := platformRuntimeStop(ctx); stopErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("pc: failed to stop tailscaled service: %w", stopErr))
	}
	cleanupErr = errors.Join(cleanupErr, removeTokenFile())
	if cleanupErr != nil {
		return cleanupErr
	}
	logrus.Infoln("pc: tailscale logout and service stop completed")
	return nil
}

func waitForLoggedOut(ctx context.Context, path string) error {
	statusCtx, cancel := context.WithTimeout(ctx, loggedOutWaitTimeout)
	defer cancel()

	ticker := time.NewTicker(loggedOutPollInterval)
	defer ticker.Stop()
	for {
		probeCtx, probeCancel := context.WithTimeout(statusCtx, loggedOutProbeTimeout)
		loggedOut := tailscaleLoggedOut(probeCtx, path)
		probeCancel()
		if loggedOut {
			return nil
		}
		select {
		case <-statusCtx.Done():
			return fmt.Errorf("pc: timed out waiting for tailscale logged-out state: %w", statusCtx.Err())
		case <-ticker.C:
		}
	}
}

func tailscaleLoggedOut(ctx context.Context, path string) bool {
	var status struct {
		BackendState string `json:"BackendState"`
	}
	out, err := tailscaleCommandContext(ctx, path, "status", "--json").Output()
	if err == nil {
		err = json.Unmarshal(out, &status)
	}
	if err != nil {
		return false
	}
	return strings.EqualFold(strings.TrimSpace(status.BackendState), "NeedsLogin")
}

func tailscaleCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		privilegedArgs := append([]string{"-n", path}, args...)
		cmd = exec.CommandContext(ctx, "/usr/bin/sudo", privilegedArgs...)
	} else {
		cmd = exec.CommandContext(ctx, path, args...)
	}
	cmd.Env = tailscaleEnvironment()
	return cmd
}

// tailscaleEnvironment isolates enrollment and lifecycle commands from customer-controlled
// proxy variables while leaving ordinary stage and container proxy behavior unchanged.
func tailscaleEnvironment() []string {
	env := os.Environ()
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, found := strings.Cut(entry, "=")
		if found && isProxyEnvironmentKey(key) {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}

// ClearProxyEnvironment removes process-global proxy state inherited from an earlier stage.
func ClearProxyEnvironment() {
	for _, entry := range os.Environ() {
		key, _, found := strings.Cut(entry, "=")
		if found && isProxyEnvironmentKey(key) {
			_ = os.Unsetenv(key)
		}
	}
}

func isProxyEnvironmentKey(key string) bool {
	switch strings.ToUpper(strings.TrimSpace(key)) {
	case "HTTP_PROXY", "HTTPS_PROXY", "NO_PROXY", "ALL_PROXY":
		return true
	default:
		return false
	}
}

func wifClientID(clientID string) string {
	separator := "?"
	if strings.Contains(clientID, "?") {
		separator = "&"
	}
	return clientID + separator + "ephemeral=true&preauthorized=true"
}

func secureTokenDir(ctx context.Context) error {
	if !filepath.IsAbs(TokenDir) {
		return fmt.Errorf("pc: state directory must be absolute")
	}
	if err := os.MkdirAll(TokenDir, privateDirectoryMode); err != nil {
		return fmt.Errorf("pc: failed to create state directory: %w", err)
	}
	info, err := os.Lstat(TokenDir)
	if err != nil || !info.IsDir() {
		return fmt.Errorf("pc: state path must be a real directory")
	}
	if err := securePlatformTokenDir(ctx); err != nil {
		return err
	}
	return os.Chmod(TokenDir, privateDirectoryMode)
}

func writePrivateFile(path string, data []byte) error {
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, privateFileMode)
	if err != nil {
		return err
	}
	if _, err := file.Write(data); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return err
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return err
	}
	return nil
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	// Only a proven absence is clean; permission and I/O failures must fail closed.
	return !os.IsNotExist(err)
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pc: failed to remove %s: %w", filepath.Base(path), err)
	}
	return nil
}

func removeTokenFile() error {
	info, err := os.Lstat(TokenDir)
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil || !info.IsDir() {
		return fmt.Errorf("pc: state path must be a real directory")
	}
	return removeFile(TokenFile)
}
