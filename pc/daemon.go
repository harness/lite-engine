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

	semver "github.com/coreos/go-semver/semver"
	"github.com/sirupsen/logrus"
)

const (
	approvedTailscaleVersion = "1.98.9"
	logoutTimeout            = 30 * time.Second
	statusTimeout            = 10 * time.Second
	versionTimeout           = 5 * time.Second
	legacyInspectionTimeout  = 5 * time.Second
	runtimeStatusTimeout     = 8 * time.Second
	privateFileMode          = os.FileMode(0600)
	privateDirectoryMode     = os.FileMode(0700)
)

var (
	// ErrUnsupported is returned when the certified, prebaked runtime is unavailable.
	ErrUnsupported = fmt.Errorf(
		"pc: a stable tailscale 1.x runtime at or above %s is required",
		approvedTailscaleVersion)
	lifecycleMu sync.Mutex
)

type lifecycleState string

const (
	stateJoining  lifecycleState = "JOINING"
	stateActive   lifecycleState = "ACTIVE"
	stateCleaning lifecycleState = "CLEANING"
)

type lifecycleMarker struct {
	State             lifecycleState `json:"state"`
	BindingGeneration uint64         `json:"bindingGeneration"`
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

// RuntimeStatus is called only when DRA explicitly requests the PC health contract.
func RuntimeStatus(ctx context.Context) (string, bool) {
	if !supportedPlatform() || !platformRuntimeReady() {
		return "", false
	}
	statusCtx, cancel := context.WithTimeout(ctx, runtimeStatusTimeout)
	defer cancel()
	path, err := tailscalePath()
	if err != nil {
		return "", false
	}
	version, versionKnown := tailscaleVersion(statusCtx, path)
	if !versionKnown {
		return "", false
	}
	return version, runtimeClean(statusCtx, path)
}

func runtimeClean(ctx context.Context, path string) bool {
	if markerExists() || tokenFileExists() || fileExists(cleanupMarkerPath()) || WasUsed() ||
		platformNetworkResidue() || legacyEgressResidue(ctx) {
		return false
	}
	loggedIn, known := tailscaleLoginState(ctx, path)
	return known && !loggedIn
}

func tailscaleVersion(ctx context.Context, path string) (string, bool) {
	ctx, cancel := context.WithTimeout(ctx, versionTimeout)
	defer cancel()
	out, err := tailscaleCommandContext(ctx, path, "version").CombinedOutput()
	if err != nil {
		return "", false
	}
	line := strings.TrimPrefix(strings.TrimSpace(strings.Split(string(out), "\n")[0]), "v")
	version, err := semver.NewVersion(line)
	if err != nil {
		return "", false
	}
	return version.String(), true
}

// JoinAndConfigure joins the host to the customer network. Routes are owned by tailscaled.
// Tailscaled also owns host DNS on Linux and Windows; the open-source macOS runtime requires the
// bounded platform snapshot/apply/restore hook. Container-only MTU/DNS is applied by engine setup.
func JoinAndConfigure(ctx context.Context, cfg *Config) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	if err := Validate(cfg, time.Now()); err != nil {
		return err
	}
	path, err := tailscalePath()
	if err != nil {
		return ErrUnsupported
	}
	version, ok := tailscaleVersion(ctx, path)
	if !ok || !supportedTailscaleVersion(version) {
		return ErrUnsupported
	}
	if !runtimeClean(ctx, path) {
		return fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden")
	}
	logrus.WithFields(logrus.Fields{
		"tailscale_version":  version,
		"binding_generation": cfg.BindingGeneration,
		"hostname":           cfg.Hostname,
	}).Infoln("pc: runtime preflight completed")
	if err := secureTokenDir(); err != nil {
		return err
	}
	if err := writeMarker(lifecycleMarker{State: stateJoining, BindingGeneration: cfg.BindingGeneration}); err != nil {
		return err
	}
	if err := platformNetworkPrepare(ctx, path); err != nil {
		return errors.Join(
			fmt.Errorf("pc: failed to prepare platform networking: %w", err),
			removeFile(MarkerFile),
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
	out, joinErr := tailscaleCommandContext(joinCtx, path, args...).CombinedOutput()
	if joinErr != nil {
		safeOutput := strings.ReplaceAll(string(out), cfg.OIDCToken, "[REDACTED]")
		logrus.WithError(joinErr).WithField("output", safeOutput).Errorln("pc: tailscale up failed")
		return rollbackSetup(ctx, fmt.Errorf("pc: tailscale up failed: %w", joinErr))
	}
	if err := confirmJoined(ctx, path); err != nil {
		return rollbackSetup(ctx, err)
	}
	if err := platformNetworkActivate(ctx, path); err != nil {
		return rollbackSetup(ctx, fmt.Errorf("pc: failed to activate platform networking: %w", err))
	}
	if err := removeFile(TokenFile); err != nil {
		return rollbackSetup(ctx, err)
	}
	if err := writeMarker(lifecycleMarker{State: stateActive, BindingGeneration: cfg.BindingGeneration}); err != nil {
		return rollbackSetup(ctx, err)
	}
	if err := writeFileAtomically(usedMarkerPath(), []byte("1\n")); err != nil {
		return rollbackSetup(ctx, err)
	}
	logrus.WithField("hostname", cfg.Hostname).Infoln("pc: joined customer network")
	return nil
}

func supportedTailscaleVersion(value string) bool {
	version, err := semver.NewVersion(strings.TrimPrefix(strings.TrimSpace(value), "v"))
	if err != nil || version.Major != 1 || version.PreRelease != "" {
		return false
	}
	minimum := semver.New(approvedTailscaleVersion)
	return version.Compare(*minimum) >= 0
}

func rollbackSetup(ctx context.Context, cause error) error {
	if cleanupErr := logoutUnlocked(ctx); cleanupErr != nil {
		return errors.Join(cause, fmt.Errorf("pc: setup rollback failed: %w", cleanupErr))
	}
	return cause
}

// Logout removes the authenticated session. The daemon remains running and idle for VM reuse.
func Logout(ctx context.Context) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()
	return logoutUnlocked(ctx)
}

func logoutUnlocked(ctx context.Context) error {
	if err := secureTokenDir(); err != nil {
		return err
	}
	marker := lifecycleMarker{State: stateCleaning}
	if existing, err := readMarker(); err == nil {
		marker.BindingGeneration = existing.BindingGeneration
	}
	if err := writeMarker(marker); err != nil {
		return err
	}
	if err := writeFileAtomically(cleanupMarkerPath(), []byte("1\n")); err != nil {
		return err
	}

	cleanupCtx := context.WithoutCancel(ctx)
	path, pathErr := tailscalePath()
	logrus.Infoln("pc: starting tailscale logout")
	var cleanupErr error
	if pathErr != nil {
		cleanupErr = ErrUnsupported
	} else {
		logoutCtx, cancel := context.WithTimeout(cleanupCtx, logoutTimeout)
		_, logoutErr := tailscaleCommandContext(logoutCtx, path, "logout").CombinedOutput()
		cancel()
		if logoutErr != nil {
			cleanupErr = fmt.Errorf("pc: tailscale logout failed: %w", logoutErr)
		} else {
			loggedIn, stateKnown := tailscaleLoginState(cleanupCtx, path)
			if !stateKnown || loggedIn {
				cleanupErr = fmt.Errorf(
					"pc: tailscale logout completed but clean logged-out state could not be confirmed")
			}
		}
	}
	// Restore platform-owned state even if logout failed. The durable cleanup marker remains until
	// both operations succeed, so DRA will discard rather than reuse an uncertain VM.
	if restoreErr := platformNetworkRestore(cleanupCtx); restoreErr != nil {
		cleanupErr = errors.Join(cleanupErr, fmt.Errorf("pc: failed to restore platform networking: %w", restoreErr))
	}
	if cleanupErr != nil {
		return cleanupErr
	}
	if err := removeFile(TokenFile); err != nil {
		return err
	}
	if err := removeFile(MarkerFile); err != nil {
		return err
	}
	if err := removeFile(cleanupMarkerPath()); err != nil {
		return err
	}
	logrus.Infoln("pc: tailscale logout and clean-state verification completed")
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
	statusCtx, cancel := context.WithTimeout(ctx, statusTimeout)
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
	case "running":
		return true, true
	case "needslogin", "nostate", "stopped":
		return false, true
	default:
		return false, false
	}
}

func tailscaleCommandContext(ctx context.Context, path string, args ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, args...)
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

func readMarker() (lifecycleMarker, error) {
	var marker lifecycleMarker
	data, err := os.ReadFile(MarkerFile)
	if err != nil {
		return marker, err
	}
	if err := json.Unmarshal(data, &marker); err != nil {
		return marker, err
	}
	return marker, nil
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
	info, err := os.Lstat(path)
	return err == nil && info.Mode().IsRegular()
}

func removeFile(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("pc: failed to remove %s: %w", filepath.Base(path), err)
	}
	return nil
}
