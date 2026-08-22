// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package pc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/netip"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"
)

const (
	logoutTimeout           = 30 * time.Second
	statusTimeout           = 10 * time.Second
	joinedStatusTimeout     = 5 * time.Second
	legacyInspectionTimeout = 5 * time.Second
	runtimeStatusTimeout    = 8 * time.Second
	runtimeServiceTimeout   = 30 * time.Second
	loggedOutPollInterval   = 200 * time.Millisecond
	privateFileMode         = os.FileMode(0600)
	privateDirectoryMode    = os.FileMode(0700)
	privilegedExtraArgs     = 2
)

var (
	// ErrUnsupported is returned when the installed Tailscale CLI or service is unavailable.
	ErrUnsupported = fmt.Errorf("pc: an installed tailscale runtime is required")
	lifecycleMu    sync.Mutex
)

type lifecycleState string

const (
	stateJoining  lifecycleState = "JOINING"
	stateActive   lifecycleState = "ACTIVE"
	stateCleaning lifecycleState = "CLEANING"
)

type lifecycleMarker struct {
	State lifecycleState `json:"state"`
}

func cleanupMarkerPath() string {
	return filepath.Join(TokenDir, "cleanup-incomplete")
}

func usedMarkerPath() string {
	return filepath.Join(TokenDir, "pc-used")
}

// WasUsed reports whether this VM has entered the PC lifecycle and still requires a complete
// stage-resource cleanup proof before reuse.
func WasUsed() bool {
	return fileExists(usedMarkerPath())
}

// MarkCleanupComplete clears the durable reuse fence only after the handler has completed both
// strict stage-resource cleanup and Tailscale logout.
func MarkCleanupComplete() error {
	return removeFile(usedMarkerPath())
}

func supportedPlatform() bool {
	switch runtime.GOOS {
	case "linux":
		return runtime.GOARCH == "amd64" || runtime.GOARCH == "arm64"
	case "windows":
		return runtime.GOARCH == "amd64"
	case "darwin":
		return runtime.GOARCH == "arm64"
	default:
		return false
	}
}

// RuntimeClean reports whether the baked runtime is available, stopped, and free of lifecycle
// residue. It does not start tailscaled or enforce a product version.
func RuntimeClean(ctx context.Context) bool {
	if !supportedPlatform() || !platformRuntimeReady() {
		return false
	}
	statusCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	if _, err := tailscalePath(); err != nil {
		return false
	}
	return localRuntimeClean(statusCtx)
}

func localRuntimeClean(ctx context.Context) bool {
	if markerExists() || tokenFileExists() || fileExists(cleanupMarkerPath()) || WasUsed() ||
		platformNetworkResidue() || legacyEgressResidue(ctx) {
		return false
	}
	running, known := platformRuntimeRunning(ctx)
	return known && !running
}

// JoinAndConfigure joins the host to the customer network. Host routes and DNS are owned by
// tailscaled on every supported platform. Container-only MTU/DNS is applied by engine setup.
func JoinAndConfigure(ctx context.Context, cfg *Config) error { //nolint:gocyclo,funlen // Ordered fail-closed lifecycle.
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	if err := Validate(cfg, time.Now()); err != nil {
		return err
	}
	if !supportedPlatform() {
		return ErrUnsupported
	}
	path, err := tailscalePath()
	if err != nil {
		return ErrUnsupported
	}
	if !localRuntimeClean(ctx) {
		return fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden")
	}
	serviceStart := time.Now()
	if err := platformRuntimeStart(ctx); err != nil {
		logrus.WithError(err).
			WithField("latency", time.Since(serviceStart)).
			Errorln("pc: tailscaled service start failed")
		return errors.Join(
			fmt.Errorf("pc: failed to start tailscaled service: %w", err),
			platformRuntimeStop(context.WithoutCancel(ctx)),
		)
	}
	logrus.WithField("latency", time.Since(serviceStart)).Infoln("pc: tailscaled service start completed")
	if waitErr := waitForLoggedOut(ctx, path); waitErr != nil {
		stopErr := platformRuntimeStop(context.WithoutCancel(ctx))
		return errors.Join(
			fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden: %w", waitErr),
			stopErr,
		)
	}
	logrus.WithField("hostname", cfg.Hostname).Infoln("pc: installed runtime is ready for join")
	if err := secureTokenDir(); err != nil {
		return errors.Join(err, platformRuntimeStop(context.WithoutCancel(ctx)))
	}
	if err := writeMarker(lifecycleMarker{State: stateJoining}); err != nil {
		return errors.Join(err, platformRuntimeStop(context.WithoutCancel(ctx)))
	}
	if err := platformNetworkPrepare(ctx, path); err != nil {
		return errors.Join(
			fmt.Errorf("pc: failed to prepare platform networking: %w", err),
			removeFile(MarkerFile),
			platformRuntimeStop(context.WithoutCancel(ctx)),
		)
	}
	if err := writeFileAtomically(TokenFile, []byte(cfg.OIDCToken)); err != nil {
		return rollbackSetup(ctx, fmt.Errorf("pc: failed to write OIDC token file: %w", err))
	}

	joinCtx, cancel := context.WithTimeout(ctx, JoinTimeout)
	defer cancel()
	args := []string{
		"up",
		"--client-id=" + wifClientID(cfg.ClientID),
		"--id-token=file:" + TokenFile,
		"--advertise-tags=" + cfg.Tag,
		"--accept-routes",
		"--accept-dns=true",
		"--hostname=" + cfg.Hostname,
		fmt.Sprintf("--timeout=%ds", int(JoinTimeout.Seconds())),
	}
	// Windows otherwise associates the session with an interactive user. Hosted VMs run headless,
	// so keep the system service connected for the full stage lifetime.
	if runtime.GOOS == "windows" {
		args = append(args, "--unattended=true")
	}
	joinStart := time.Now()
	out, joinErr := tailscaleCommandContext(joinCtx, path, args...).CombinedOutput()
	if joinErr != nil {
		safeOutput := strings.ReplaceAll(string(out), cfg.OIDCToken, "[REDACTED]")
		logrus.WithError(joinErr).
			WithField("latency", time.Since(joinStart)).
			WithField("output", safeOutput).
			Errorln("pc: tailscale up failed")
		return rollbackSetup(ctx, fmt.Errorf("pc: tailscale up failed: %w (%s)", joinErr, strings.TrimSpace(safeOutput)))
	}
	logrus.WithField("latency", time.Since(joinStart)).Infoln("pc: tailscale up completed")
	confirmationStart := time.Now()
	if err := confirmJoined(ctx, path); err != nil {
		logrus.WithError(err).
			WithField("latency", time.Since(confirmationStart)).
			Errorln("pc: joined-state confirmation failed")
		return rollbackSetup(ctx, err)
	}
	logrus.WithField("latency", time.Since(confirmationStart)).Infoln("pc: joined-state confirmation completed")
	if err := platformNetworkActivate(ctx, path); err != nil {
		return rollbackSetup(ctx, fmt.Errorf("pc: failed to activate platform networking: %w", err))
	}
	if err := removeTokenFile(); err != nil {
		return rollbackSetup(ctx, err)
	}
	if err := writeMarker(lifecycleMarker{State: stateActive}); err != nil {
		return rollbackSetup(ctx, err)
	}
	if err := writeFileAtomically(usedMarkerPath(), []byte("1\n")); err != nil {
		return rollbackSetup(ctx, err)
	}
	logrus.WithField("hostname", cfg.Hostname).Infoln("pc: joined customer network")
	return nil
}

func rollbackSetup(ctx context.Context, cause error) error {
	// A failed PC setup is outcome-indeterminate and DRA always discards the VM. On
	// Windows, the Tailscale service can reject self-logout because its LocalSystem
	// caller does not own the generated login profile. Disposal cleanup may defer
	// removal of that already-ephemeral node after stopping the service; reusable
	// cleanup remains strict.
	if cleanupErr := logoutUnlocked(ctx, true); cleanupErr != nil {
		return errors.Join(cause, fmt.Errorf("pc: setup rollback failed: %w", cleanupErr))
	}
	return cause
}

// Logout removes the authenticated session and stops the OS-managed daemon before VM reuse.
func Logout(ctx context.Context) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	return logoutUnlocked(ctx, false)
}

// LogoutForDisposal performs terminal cleanup for a VM that DRA will destroy. It differs from
// Logout only for the documented Windows LocalSystem profile-ownership failure: the service is
// stopped and the already-ephemeral node is allowed to age out instead of making VM destruction
// report a false cleanup failure. It must never be used before suspend or pool reuse.
func LogoutForDisposal(ctx context.Context) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	return logoutUnlocked(ctx, true)
}

func logoutUnlocked(ctx context.Context, allowDeferredWindowsRemoval bool) (resultErr error) { //nolint:gocyclo // Ordered cleanup.
	// The raw JWT is never needed for logout. Always attempt its deletion, including when an
	// earlier cleanup phase fails and the durable reuse fence must remain.
	defer func() {
		if tokenErr := removeTokenFile(); tokenErr != nil {
			resultErr = errors.Join(resultErr, tokenErr)
		}
	}()

	if err := secureTokenDir(); err != nil {
		return err
	}
	if err := writeMarker(lifecycleMarker{State: stateCleaning}); err != nil {
		return err
	}
	if err := writeFileAtomically(cleanupMarkerPath(), []byte("1\n")); err != nil {
		return err
	}

	cleanupCtx := context.WithoutCancel(ctx)
	path, pathErr := tailscalePath()
	logrus.Infoln("pc: starting tailscale logout")
	var cleanupErr error
	deferredEphemeralRemoval := false
	if pathErr != nil {
		cleanupErr = ErrUnsupported
	} else {
		if startErr := platformRuntimeStart(cleanupCtx); startErr != nil {
			cleanupErr = fmt.Errorf("pc: failed to start tailscaled for cleanup: %w", startErr)
		} else {
			logoutCtx, cancel := context.WithTimeout(cleanupCtx, logoutTimeout)
			logoutOutput, logoutErr := tailscaleCommandContext(logoutCtx, path, "logout").CombinedOutput()
			cancel()
			if logoutErr != nil {
				// Tailscale's Windows service currently creates an unattended profile that
				// its LocalSystem CLI actor cannot later disconnect. The node is already
				// ephemeral (the WIF client ID requests ephemeral=true), so terminal VM
				// disposal can safely stop the service and let the control plane remove the
				// offline node. Never accept this for suspend/reuse, and never expose the
				// command output in logs or returned errors.
				if allowDeferredWindowsRemoval && runtime.GOOS == "windows" &&
					strings.Contains(strings.ToLower(string(logoutOutput)),
						"target profile does not belong to the user") {
					deferredEphemeralRemoval = true
					logrus.Warnln(
						"pc: Windows profile ownership prevented immediate logout; stopping the service and deferring removal of the ephemeral node")
				} else {
					cleanupErr = fmt.Errorf("pc: tailscale logout failed: %w", logoutErr)
				}
			} else if confirmErr := waitForLoggedOut(cleanupCtx, path); confirmErr != nil {
				cleanupErr = fmt.Errorf(
					"pc: tailscale logout completed but clean logged-out state could not be confirmed: %w",
					confirmErr)
			}
		}
	}
	// Restore platform-owned state even if logout failed. The durable cleanup marker remains until
	// both operations succeed, so DRA will discard rather than reuse an uncertain VM.
	if restoreErr := platformNetworkRestore(cleanupCtx); restoreErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("pc: failed to restore platform networking: %w", restoreErr))
	}
	if stopErr := platformRuntimeStop(cleanupCtx); stopErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("pc: failed to stop tailscaled service: %w", stopErr))
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	if err := removeFile(MarkerFile); err != nil {
		return err
	}
	if err := removeFile(cleanupMarkerPath()); err != nil {
		return err
	}
	if deferredEphemeralRemoval {
		logrus.Infoln("pc: tailscaled service stopped; ephemeral node removal deferred to the Tailscale control plane")
	} else {
		logrus.Infoln("pc: tailscale logout, clean-state verification, and service stop completed")
	}
	return nil
}

// NeedsNetworkCleanup is a cheap lifecycle gate used by destroy/suspend.
func NeedsNetworkCleanup() bool {
	if markerExists() || tokenFileExists() || fileExists(cleanupMarkerPath()) || platformNetworkResidue() {
		return true
	}
	if !fileExists(usedMarkerPath()) {
		return false
	}
	path, err := tailscalePath()
	if err != nil {
		return true
	}
	loggedIn, known := tailscaleLoginState(context.Background(), path)
	return !known || loggedIn
}

func confirmJoined(ctx context.Context, path string) error {
	statusCtx, cancel := context.WithTimeout(ctx, joinedStatusTimeout)
	defer cancel()
	out, err := tailscaleCommandContext(statusCtx, path, "status", "--json").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: failed to confirm tailscale join: %w", err)
	}
	var status struct {
		BackendState string `json:"BackendState"`
		Self         struct {
			Online       bool     `json:"Online"`
			TailscaleIPs []string `json:"TailscaleIPs"`
		} `json:"Self"`
	}
	if err := json.Unmarshal(out, &status); err != nil {
		return fmt.Errorf("pc: failed to parse tailscale status: %w", err)
	}
	if !strings.EqualFold(status.BackendState, "Running") || !status.Self.Online {
		return fmt.Errorf("pc: tailscale did not become online")
	}
	tailnetRange := netip.MustParsePrefix("100.64.0.0/10")
	for _, raw := range status.Self.TailscaleIPs {
		if address, parseErr := netip.ParseAddr(raw); parseErr == nil && tailnetRange.Contains(address) {
			return nil
		}
	}
	return fmt.Errorf("pc: tailscale joined without a tailnet IPv4 address")
}

func waitForLoggedOut(ctx context.Context, path string) error {
	statusCtx, cancel := context.WithTimeout(ctx, joinedStatusTimeout)
	defer cancel()

	ticker := time.NewTicker(loggedOutPollInterval)
	defer ticker.Stop()
	for {
		loggedIn, known := tailscaleLoginState(statusCtx, path)
		if known && !loggedIn {
			return nil
		}
		select {
		case <-statusCtx.Done():
			return statusCtx.Err()
		case <-ticker.C:
		}
	}
}

func tailscaleLoginState(ctx context.Context, path string) (loggedIn, known bool) {
	ctx, cancel := context.WithTimeout(ctx, statusTimeout)
	defer cancel()
	out, err := tailscaleCommandContext(ctx, path, "status", "--json").CombinedOutput()
	if err != nil {
		return false, false
	}
	var status struct {
		BackendState string `json:"BackendState"`
	}
	if json.Unmarshal(out, &status) != nil {
		return false, false
	}
	switch strings.ToLower(strings.TrimSpace(status.BackendState)) {
	case "running", "starting", "needsmachineauth", "stopped", "inuseotheruser":
		return true, true
	case "needslogin", "nostate":
		return false, true
	default:
		return false, false
	}
}

func tailscaleCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	var cmd *exec.Cmd
	if runtime.GOOS == "darwin" && os.Geteuid() != 0 {
		privilegedArgs := make([]string, 0, len(args)+privilegedExtraArgs)
		privilegedArgs = append(privilegedArgs, "-n", path)
		privilegedArgs = append(privilegedArgs, args...)
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

func secureTokenDir() error {
	if !filepath.IsAbs(TokenDir) {
		return fmt.Errorf("pc: state directory must be absolute")
	}
	if err := os.MkdirAll(TokenDir, privateDirectoryMode); err != nil {
		return fmt.Errorf("pc: failed to create state directory: %w", err)
	}
	if err := securePlatformTokenDir(); err != nil {
		return err
	}
	info, err := os.Lstat(TokenDir)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pc: state path must be a real directory")
	}
	return os.Chmod(TokenDir, privateDirectoryMode)
}

func writeMarker(marker lifecycleMarker) error {
	data, err := json.Marshal(marker)
	if err != nil {
		return err
	}
	return writeFileAtomically(MarkerFile, data)
}

func writeFileAtomically(path string, data []byte) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".pc-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if err := tmp.Chmod(privateFileMode); err != nil {
		_ = tmp.Close()
		return err
	}
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return replaceFileAtomically(tmpName, path)
}

func markerExists() bool {
	return fileExists(MarkerFile)
}

func tokenFileExists() bool {
	return fileExists(TokenFile)
}

func fileExists(path string) bool {
	_, err := os.Lstat(path)
	return err == nil
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
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return fmt.Errorf("pc: state path must be a real directory")
	}
	return removeFile(TokenFile)
}
