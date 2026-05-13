// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	osruntime "runtime"
	"strings"
	"syscall"
)

// macReadOnlyRoots lists path prefixes that are read-only or SIP-protected
// on macOS (sealed system volume) and therefore cannot be used as host volume
// mount sources. Sub-paths inside these roots cannot be created by lite-engine.
//
// Note: this is intentionally conservative — only well-known SIP/system roots
// are listed. Top-level paths that don't exist (e.g. /shared, /data) are caught
// separately by the "uncreatable top-level" check below, since the macOS root
// volume itself is read-only and `mkdir /<x>` will always fail there.
var macReadOnlyRoots = []string{
	"/System",
	"/bin",
	"/sbin",
	"/usr", // /usr/local is writable; see macWritableExceptions
	"/Library",
	"/Applications",
	"/cores",
	"/private/var/db",
	"/private/etc",
}

// macWritableExceptions are paths inside a macReadOnlyRoots prefix that are
// nevertheless writable. They are checked before the read-only roots.
var macWritableExceptions = []string{
	"/usr/local",
}

// macSuggestedWritablePaths is the human-readable hint shown in error messages.
const macSuggestedWritablePaths = "/private/tmp/<your-dir>, /Users/<user>/<your-dir>, or /private/var/folders/<...>"

// validateHostVolumePath returns an error if `path` cannot safely be used as a
// host volume mount source on the current OS. On macOS this catches the most
// common cause of cryptic lite-engine setup failures (CI-22685): a sharedPaths
// entry that maps to a SIP-protected root or to a top-level directory under "/"
// that does not exist (since the root volume is read-only on a sealed APFS
// system volume, `mkdir /<x>` will always fail there).
//
// On non-Darwin platforms this is a no-op — Linux runners and BYOI containers
// are expected to be writable, and the existing setup code is the authority.
func validateHostVolumePath(path string) error {
	if osruntime.GOOS != "darwin" {
		return nil
	}
	return validateHostVolumePathDarwin(path)
}

// validateHostVolumePathDarwin contains the macOS-specific checks. Exposed
// (lowercase, package-private) so tests can exercise it on any OS.
func validateHostVolumePathDarwin(path string) error {
	if path == "" {
		return nil
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		// Relative paths are not host paths — let the existing code handle them.
		return nil
	}

	// 1. Allow explicit writable exceptions inside otherwise-read-only roots.
	for _, ex := range macWritableExceptions {
		if isPathUnder(clean, ex) {
			return nil
		}
	}

	// 2. Reject known SIP / read-only roots with a precise reason.
	for _, root := range macReadOnlyRoots {
		if isPathUnder(clean, root) {
			return newProtectedPathError(path, root, "is inside a read-only / SIP-protected location on macOS")
		}
	}

	// 3. Reject paths whose top-level component under "/" does not exist on
	//    the host. On a sealed macOS system volume "/" itself is read-only,
	//    so `mkdir /<top>` is guaranteed to fail with EROFS. This is what
	//    happened to Invex with /shared/coverage_reports.
	topLevel := topLevelComponent(clean)
	if topLevel != "" && topLevel != "/" {
		if _, err := os.Stat(topLevel); errors.Is(err, os.ErrNotExist) {
			return newProtectedPathError(
				path,
				topLevel,
				fmt.Sprintf("requires creating %q at the macOS root, which is read-only on the sealed system volume", topLevel),
			)
		}
	}

	return nil
}

// enrichHostVolumeMkdirError wraps the raw os.MkdirAll error returned by the
// kernel with a clear, actionable message when the underlying cause is a
// read-only filesystem (EROFS). This is defense-in-depth for paths that the
// static validation above did not catch (e.g. user-mounted read-only volumes).
//
// The original error is preserved via %w so callers can still use errors.Is.
func enrichHostVolumeMkdirError(path string, err error) error {
	if err == nil {
		return nil
	}
	if !isReadOnlyFSError(err) {
		return fmt.Errorf("failed to create directory for host volume path: %q: %w", path, err)
	}
	return fmt.Errorf(
		"failed to create directory for host volume path: %q: the target filesystem is read-only. "+
			"On macOS this typically means the path is on the sealed system volume or under a SIP-protected root. "+
			"Use a writable location such as %s instead. Original error: %w",
		path, macSuggestedWritablePaths, err,
	)
}

// newProtectedPathError builds the user-facing error returned by the
// pre-MkdirAll validator. offendingPrefix is the matched read-only / unwritable
// prefix and is included to make the failure easy to diagnose at a glance.
func newProtectedPathError(originalPath, offendingPrefix, reason string) error {
	return fmt.Errorf(
		"invalid host volume / sharedPaths entry %q on macOS: path %s (matched: %q). "+
			"Use a writable location such as %s instead",
		originalPath, reason, offendingPrefix, macSuggestedWritablePaths,
	)
}

// isPathUnder reports whether `path` is exactly `prefix` or lives inside it.
// Both arguments are expected to be cleaned absolute paths.
func isPathUnder(path, prefix string) bool {
	if path == prefix {
		return true
	}
	if !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	return strings.HasPrefix(path, prefix)
}

// topLevelComponent returns "/<first>" for an absolute path. For "/" it
// returns "/". For relative paths it returns "".
func topLevelComponent(path string) string {
	if !strings.HasPrefix(path, "/") {
		return ""
	}
	trimmed := strings.TrimPrefix(path, "/")
	if trimmed == "" {
		return "/"
	}
	if i := strings.Index(trimmed, "/"); i >= 0 {
		return "/" + trimmed[:i]
	}
	return "/" + trimmed
}

// isReadOnlyFSError reports whether err is (or wraps) syscall.EROFS.
// Works regardless of whether the error came from os.MkdirAll directly or
// through a *os.PathError wrapper.
func isReadOnlyFSError(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, syscall.EROFS) {
		return true
	}
	// Fallback for platforms where errors.Is(EROFS) doesn't cover wrapped
	// PathErrors — match on the substring of the underlying error message.
	return strings.Contains(strings.ToLower(err.Error()), "read-only file system")
}
