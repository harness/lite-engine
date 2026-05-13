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
	"testing"
)

// TestValidateHostVolumePathDarwin exercises the macOS-specific validator
// directly so the test runs on any OS in CI. The bare `validateHostVolumePath`
// gates on runtime.GOOS and is covered by a separate test below.
func TestValidateHostVolumePathDarwin(t *testing.T) {
	// "/" is guaranteed to exist on every host (linux, darwin, even Windows
	// in MSYS), and "/private/tmp" / "/Users" exist on macOS. We pick test
	// inputs that don't depend on the host's filesystem layout, except for
	// one explicit case that we conditionally enable below.
	tests := []struct {
		name      string
		path      string
		wantErr   bool
		wantInMsg string
	}{
		{
			name:    "empty path is allowed",
			path:    "",
			wantErr: false,
		},
		{
			name:    "relative path is left to existing logic",
			path:    "relative/path",
			wantErr: false,
		},
		{
			name:    "writable exception inside /usr is allowed",
			path:    "/usr/local/share/build",
			wantErr: false,
		},
		{
			name:      "rejects /System (SIP)",
			path:      "/System/Library/something",
			wantErr:   true,
			wantInMsg: "/System",
		},
		{
			name:      "rejects /usr (except /usr/local)",
			path:      "/usr/lib/foo",
			wantErr:   true,
			wantInMsg: "/usr",
		},
		{
			name:      "rejects /Library",
			path:      "/Library/Caches/foo",
			wantErr:   true,
			wantInMsg: "/Library",
		},
		{
			name:      "rejects /bin",
			path:      "/bin/whatever",
			wantErr:   true,
			wantInMsg: "/bin",
		},
		{
			name:      "rejects /private/etc (system config)",
			path:      "/private/etc/foo",
			wantErr:   true,
			wantInMsg: "/private/etc",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			err := validateHostVolumePathDarwin(tc.path)
			if (err != nil) != tc.wantErr {
				t.Fatalf("validateHostVolumePathDarwin(%q) error = %v, wantErr=%v", tc.path, err, tc.wantErr)
			}
			if tc.wantErr && tc.wantInMsg != "" && !strings.Contains(err.Error(), tc.wantInMsg) {
				t.Fatalf("error message %q does not contain %q", err.Error(), tc.wantInMsg)
			}
			if tc.wantErr {
				if !strings.Contains(err.Error(), "macOS") {
					t.Errorf("error message should mention macOS, got: %s", err.Error())
				}
				if !strings.Contains(err.Error(), "/private/tmp") {
					t.Errorf("error message should suggest /private/tmp, got: %s", err.Error())
				}
			}
		})
	}
}

// TestValidateHostVolumePathDarwin_TopLevelMissing covers the Invex case
// (`/shared/coverage_reports`): a top-level dir under "/" that does not exist
// on the host. We synthesize a guaranteed-missing top-level name so the test
// is deterministic across machines.
func TestValidateHostVolumePathDarwin_TopLevelMissing(t *testing.T) {
	missingTop := guaranteedMissingTopLevel(t)
	path := missingTop + "/coverage_reports"

	err := validateHostVolumePathDarwin(path)
	if err == nil {
		t.Fatalf("expected error for missing top-level path %q, got nil", path)
	}
	if !strings.Contains(err.Error(), missingTop) {
		t.Errorf("error should mention the offending top-level %q: %s", missingTop, err.Error())
	}
	if !strings.Contains(err.Error(), "read-only") {
		t.Errorf("error should explain that the macOS root is read-only, got: %s", err.Error())
	}
}

// TestValidateHostVolumePath_NonDarwinIsNoop ensures the public entry point
// is a no-op on non-Darwin platforms. On Darwin we skip — the dedicated
// Darwin test above already covers behavior.
func TestValidateHostVolumePath_NonDarwinIsNoop(t *testing.T) {
	if osruntime.GOOS == "darwin" {
		t.Skip("non-Darwin behavior only")
	}
	if err := validateHostVolumePath("/System/foo"); err != nil {
		t.Fatalf("expected no-op on %s, got error: %v", osruntime.GOOS, err)
	}
}

func TestIsPathUnder(t *testing.T) {
	tests := []struct {
		path, prefix string
		want         bool
	}{
		{"/usr/local", "/usr/local", true},
		{"/usr/local/bin", "/usr/local", true},
		{"/usr/localfoo", "/usr/local", false}, // prefix-string vs path-prefix
		{"/usr", "/usr/local", false},
		{"/", "/usr", false},
	}
	for _, tc := range tests {
		t.Run(tc.path+"_under_"+tc.prefix, func(t *testing.T) {
			if got := isPathUnder(tc.path, tc.prefix); got != tc.want {
				t.Errorf("isPathUnder(%q, %q) = %v, want %v", tc.path, tc.prefix, got, tc.want)
			}
		})
	}
}

func TestTopLevelComponent(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"/", "/"},
		{"/shared", "/shared"},
		{"/shared/coverage_reports", "/shared"},
		{"/private/tmp/foo", "/private"},
		{"relative/path", ""},
		{"", ""},
	}
	for _, tc := range tests {
		t.Run(tc.in, func(t *testing.T) {
			if got := topLevelComponent(tc.in); got != tc.want {
				t.Errorf("topLevelComponent(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestEnrichHostVolumeMkdirError(t *testing.T) {
	// nil error stays nil
	if got := enrichHostVolumeMkdirError("/x", nil); got != nil {
		t.Errorf("expected nil for nil input, got %v", got)
	}

	// EROFS gets the actionable hint
	rofsErr := &os.PathError{Op: "mkdir", Path: "/shared", Err: syscall.EROFS}
	got := enrichHostVolumeMkdirError("/shared/coverage_reports", rofsErr)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	if !strings.Contains(got.Error(), "read-only") {
		t.Errorf("expected read-only hint in error, got: %s", got.Error())
	}
	if !strings.Contains(got.Error(), "/private/tmp") {
		t.Errorf("expected suggestion of /private/tmp in error, got: %s", got.Error())
	}
	if !errors.Is(got, syscall.EROFS) {
		t.Errorf("expected wrapped error to satisfy errors.Is(EROFS), got: %v", got)
	}

	// Non-EROFS errors keep the original wrapping shape
	other := errors.New("disk full")
	got = enrichHostVolumeMkdirError("/x", other)
	if got == nil {
		t.Fatal("expected non-nil error")
	}
	if strings.Contains(got.Error(), "read-only") {
		t.Errorf("did not expect read-only hint for non-EROFS error, got: %s", got.Error())
	}
	if !errors.Is(got, other) {
		t.Errorf("expected wrapped error to satisfy errors.Is(other)")
	}
}

func TestIsReadOnlyFSError(t *testing.T) {
	if isReadOnlyFSError(nil) {
		t.Error("nil should not be a read-only FS error")
	}
	if !isReadOnlyFSError(syscall.EROFS) {
		t.Error("syscall.EROFS should be detected directly")
	}
	wrapped := fmt.Errorf("mkdir failed: %w", syscall.EROFS)
	if !isReadOnlyFSError(wrapped) {
		t.Error("wrapped EROFS should be detected via errors.Is")
	}
	pathErr := &os.PathError{Op: "mkdir", Path: "/shared", Err: syscall.EROFS}
	if !isReadOnlyFSError(pathErr) {
		t.Error("PathError wrapping EROFS should be detected")
	}
	// String fallback for environments where errors.Is doesn't match.
	if !isReadOnlyFSError(errors.New("mkdir /shared: read-only file system")) {
		t.Error("string fallback should detect read-only file system")
	}
	if isReadOnlyFSError(errors.New("permission denied")) {
		t.Error("unrelated errors should not be flagged as EROFS")
	}
}

// guaranteedMissingTopLevel returns an absolute path of the form
// "/<unique-name>" that is guaranteed not to exist on the test host.
func guaranteedMissingTopLevel(t *testing.T) string {
	t.Helper()
	for i := 0; i < 20; i++ {
		name := fmt.Sprintf("/__lite_engine_ci22685_test_%d_%d", os.Getpid(), i)
		if _, err := os.Stat(name); errors.Is(err, os.ErrNotExist) {
			return name
		}
	}
	t.Fatalf("could not generate a guaranteed-missing top-level path under %q", string(filepath.Separator))
	return ""
}
