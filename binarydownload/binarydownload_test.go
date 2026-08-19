// Copyright 2026 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package binarydownload

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/klauspost/compress/zstd"
)

const (
	uncompressedURL = "https://github.com/meenaravichandran1/drone-gcs/releases/download/v1.6.3-debug/ServiceNow-0.0.1-darwin-arm64"
	compressedURL   = "https://github.com/meenaravichandran1/drone-gcs/releases/download/v1.6.3-debug/ServiceNow-darwin-arm64.zst"
)

// TestDownloadUncompressed downloads a plain binary and asserts it is present and executable.
func TestDownloadUncompressed(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ServiceNow")

	got, err := download(context.Background(), []string{uncompressedURL}, dest, false)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if got != dest {
		t.Fatalf("expected path %s, got %s", dest, got)
	}
	assertExecutable(t, got)
}

// TestDownloadCompressed downloads a .zst binary and asserts it is decompressed,
// executable, and the intermediate .zst file is removed.
func TestDownloadCompressed(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "ServiceNow")

	got, err := download(context.Background(), []string{compressedURL}, dest, true)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if got != dest {
		t.Fatalf("expected path %s, got %s", dest, got)
	}
	if strings.HasSuffix(got, ".zst") {
		t.Fatalf("expected decompressed path, got %s", got)
	}
	if _, err := os.Stat(dest + ".zst"); !os.IsNotExist(err) {
		t.Fatalf("expected .zst file to be removed, stat err: %v", err)
	}
	assertExecutable(t, got)
}

// TestDownloadCacheHit returns the existing path without re-downloading.
func TestDownloadCacheHit(t *testing.T) {
	dest := filepath.Join(t.TempDir(), "cached")
	if err := os.WriteFile(dest, []byte("existing"), dirPerm); err != nil {
		t.Fatalf("seed cache failed: %v", err)
	}

	// Bogus URL: a cache hit must not attempt a download.
	got, err := download(context.Background(), []string{"http://invalid.invalid/nope"}, dest, false)
	if err != nil {
		t.Fatalf("cache hit should not error: %v", err)
	}
	if got != dest {
		t.Fatalf("expected %s, got %s", dest, got)
	}
}

// TestDownloadCacheMissThenHit asserts the first call downloads (miss) and the
// second returns the cached path without a second request (hit).
func TestDownloadCacheMissThenHit(t *testing.T) {
	var hits int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		atomic.AddInt32(&hits, 1)
		w.Write([]byte("binary-content"))
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "ServiceNow")
	ctx := context.Background()

	// First call: cache miss -> one download.
	if _, err := download(ctx, []string{srv.URL}, dest, false); err != nil {
		t.Fatalf("first download failed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("cache miss should trigger 1 request, got %d", got)
	}

	// Second call: cache hit -> no additional download.
	if _, err := download(ctx, []string{srv.URL}, dest, false); err != nil {
		t.Fatalf("second download failed: %v", err)
	}
	if got := atomic.LoadInt32(&hits); got != 1 {
		t.Fatalf("cache hit should trigger no new request, total requests=%d", got)
	}
}

// TestDownloadInfersZstFromURL asserts a ".zst" URL is decompressed even when the
// compressed flag is false, and the intermediate ".zst" file is removed.
func TestDownloadInfersZstFromURL(t *testing.T) {
	want := []byte("decompressed-binary-content")
	var buf bytes.Buffer
	enc, err := zstd.NewWriter(&buf)
	if err != nil {
		t.Fatalf("zstd writer: %v", err)
	}
	if _, err := enc.Write(want); err != nil {
		t.Fatalf("zstd write: %v", err)
	}
	enc.Close()
	compressed := buf.Bytes()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Write(compressed)
	}))
	defer srv.Close()

	dest := filepath.Join(t.TempDir(), "plugin")
	// compressed=false, but the URL ends in ".zst" so it must still decompress.
	got, err := download(context.Background(), []string{srv.URL + "/plugin-linux-amd64.zst"}, dest, false)
	if err != nil {
		t.Fatalf("download failed: %v", err)
	}
	if got != dest {
		t.Fatalf("expected %s, got %s", dest, got)
	}
	if _, err := os.Stat(dest + ".zst"); !os.IsNotExist(err) {
		t.Fatalf("expected .zst file removed, stat err: %v", err)
	}
	data, err := os.ReadFile(dest)
	if err != nil {
		t.Fatalf("read dest: %v", err)
	}
	if !bytes.Equal(data, want) {
		t.Fatalf("decompressed content mismatch: got %q want %q", data, want)
	}
	assertExecutable(t, got)
}

// TestBuildURL asserts os/arch tokens are always substituted and the release token
// only when a version is supplied.
func TestBuildURL(t *testing.T) {
	osArch := runtime.GOOS + "-" + runtime.GOARCH
	cases := []struct {
		name     string
		template string
		release  string
		want     string
	}{
		{
			name:     "os/arch substituted without version",
			template: "https://host/plugin-{{ os }}-{{ arch }}.zst",
			release:  "",
			want:     "https://host/plugin-" + osArch + ".zst",
		},
		{
			name:     "release and os/arch substituted with version",
			template: "https://host/download/{{ release }}/plugin-{{ os }}-{{ arch }}.zst",
			release:  "v1.2.3",
			want:     "https://host/download/v1.2.3/plugin-" + osArch + ".zst",
		},
		{
			name:     "release token left untouched without version",
			template: "https://host/download/{{ release }}/plugin.zst",
			release:  "",
			want:     "https://host/download/{{ release }}/plugin.zst",
		},
		{
			name:     "literal url unchanged",
			template: "https://host/plugin.zst",
			release:  "",
			want:     "https://host/plugin.zst",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := buildURL(tc.template, tc.release); got != tc.want {
				t.Fatalf("buildURL(%q, %q) = %q, want %q", tc.template, tc.release, got, tc.want)
			}
		})
	}
}

func assertExecutable(t *testing.T, path string) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Size() == 0 {
		t.Fatalf("downloaded file %s is empty", path)
	}
	if info.Mode().Perm()&0111 == 0 {
		t.Fatalf("file %s is not executable, mode=%v", path, info.Mode())
	}
}
