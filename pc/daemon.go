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

// Supported reports whether a qualified stable major-version-1 runtime is
// present and responsive.
func Supported() bool {
	if !supportedPlatform() {
		return false
	}
	path, err := tailscalePath()
	if err != nil || !platformReady(path) {
		return false
	}
	version, ok := tailscaleVersion(path)
	return ok && supportedTailscaleVersion(version)
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
func RuntimeStatus() (string, bool) {
	if !supportedPlatform() {
		return "", false
	}
	path, err := tailscalePath()
	if err != nil || !platformReady(path) {
		return "", false
	}
	version, versionKnown := tailscaleVersion(path)
	if !versionKnown {
		return "", false
	}
	return version, runtimeClean(path)
}

func runtimeClean(path string) bool {
	if markerExists() || tokenFileExists() || fileExists(cleanupMarkerPath()) || legacyEgressResidue() {
		return false
	}
	loggedIn, known := tailscaleLoginState(path)
	return known && !loggedIn
}

func tailscaleVersion(path string) (string, bool) {
	ctx, cancel := context.WithTimeout(context.Background(), versionTimeout)
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

// JoinAndConfigure joins the host to the customer network. Platform DNS and routes are owned by
// tailscaled; container-only MTU/DNS is applied to the stage network by engine setup.
func JoinAndConfigure(ctx context.Context, cfg *Config) error {
	lifecycleMu.Lock()
	defer lifecycleMu.Unlock()

	if err := ValidateExchange(cfg, time.Now()); err != nil {
		return err
	}
	path, err := tailscalePath()
	if err != nil || !platformReady(path) {
		return ErrUnsupported
	}
	version, ok := tailscaleVersion(path)
	if !ok || !supportedTailscaleVersion(version) {
		return ErrUnsupported
	}
	if !runtimeClean(path) {
		return fmt.Errorf("pc: runtime is dirty; VM reuse is forbidden")
	}
	if err := secureTokenDir(); err != nil {
		return err
	}
	if err := writeMarker(lifecycleMarker{State: stateJoining, BindingGeneration: cfg.BindingGeneration}); err != nil {
		return err
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
	out, joinErr := tailscaleCommandContext(joinCtx, path, args...).CombinedOutput()
	if joinErr != nil {
		safeOutput := strings.ReplaceAll(string(out), cfg.OIDCToken, "[REDACTED]")
		logrus.WithError(joinErr).WithField("output", safeOutput).Errorln("pc: tailscale up failed")
		return rollbackSetup(ctx, fmt.Errorf("pc: tailscale up failed: %w", joinErr))
	}
	if err := confirmJoined(ctx, path); err != nil {
		return rollbackSetup(ctx, err)
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

	path, err := tailscalePath()
	if err != nil || !platformReady(path) {
		return ErrUnsupported
	}
	logoutCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), logoutTimeout)
	defer cancel()
	_, err = tailscaleCommandContext(logoutCtx, path, "logout").CombinedOutput()
	if err != nil {
		return fmt.Errorf("pc: tailscale logout failed: %w", err)
	}
	loggedIn, stateKnown := tailscaleLoginState(path)
	if !stateKnown || loggedIn {
		return fmt.Errorf("pc: tailscale logout completed but clean logged-out state could not be confirmed")
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
	return nil
}

// NeedsNetworkCleanup is a cheap lifecycle gate used by destroy/suspend.
func NeedsNetworkCleanup() bool {
	if markerExists() || tokenFileExists() || fileExists(cleanupMarkerPath()) {
		return true
	}
	if !fileExists(usedMarkerPath()) {
		return false
	}
	path, err := tailscalePath()
	if err != nil || !platformReady(path) {
		return true
	}
	loggedIn, known := tailscaleLoginState(path)
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

func tailscaleLoginState(path string) (loggedIn, known bool) {
	ctx, cancel := context.WithTimeout(context.Background(), statusTimeout)
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
	if runtime.GOOS == "darwin" {
		cmd.Env = append(os.Environ(), "TAILSCALE_BE_CLI=1")
	}
	return cmd
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
