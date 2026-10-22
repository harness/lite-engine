// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

package attachments

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBuildPushURL(t *testing.T) {
	tests := []struct {
		name, base, path, want string
	}{
		{"plain", "https://har.example.com", "p/v1/test-attachments/s/login/trace.zip",
			"https://har.example.com/pkg/acc/reg/files/p/v1/test-attachments/s/login/trace.zip"},
		{"no scheme and trailing slash", "har.example.com/", "p/f.png",
			"https://har.example.com/pkg/acc/reg/files/p/f.png"},
		{"escapes each segment", "https://h", "p/100%.png", "https://h/pkg/acc/reg/files/p/100%25.png"},
		{"escapes an encoded slash", "https://h", "p/a%2Fb", "https://h/pkg/acc/reg/files/p/a%252Fb"},
		{"keeps spaces", "https://h", "p/a b.png", "https://h/pkg/acc/reg/files/p/a%20b.png"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := buildPushURL(tt.base, "acc", "reg", tt.path)
			require.NoError(t, err)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestCleanDestination(t *testing.T) {
	for _, ok := range []string{"a/b.png", "login-chromium/trace.zip", "a"} {
		_, err := cleanDestination(ok)
		assert.NoError(t, err, ok)
	}
	for _, bad := range []string{"", "/abs", "../x", "a/../b", "a//b", "a/./b", `a\b`, "a/"} {
		_, err := cleanDestination(bad)
		assert.Error(t, err, bad)
	}
}

func TestResolveUploads(t *testing.T) {
	ws := newWorkspace(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600))
	good := filepath.Join(ws, "test-results", "home-chromium", "video.webm")

	files := []file{
		{sourcePath: good, destination: "home-chromium/video.webm"},
		{sourcePath: good, destination: "home-chromium/video.webm"},                     // duplicate destination
		{sourcePath: good, destination: "../escape"},                                    // bad destination
		{sourcePath: filepath.Join(outside, "secret.txt"), destination: "x/secret.txt"}, // outside workspace
		{sourcePath: filepath.Join(ws, "missing.txt"), destination: "x/missing.txt"},
		{sourcePath: filepath.Join(ws, "test-results"), destination: "x/dir"}, // not a regular file
	}
	uploads, invalid := resolveUploads(ws, "/pipe/v1/test-attachments/step", files)

	require.Len(t, uploads, 1)
	assert.Equal(t, "/pipe/v1/test-attachments/step/home-chromium/video.webm", uploads[0].artifactPath)
	assert.Equal(t, filepath.Join("test-results", "home-chromium", "video.webm"), uploads[0].relPath)
	assert.Len(t, invalid, 5)
}

func TestResolveUploadsRejectsSymlinkOutOfWorkspace(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevated rights on Windows")
	}
	ws := newWorkspace(t)
	outside := t.TempDir()
	secret := filepath.Join(outside, "secret.txt")
	require.NoError(t, os.WriteFile(secret, []byte("s"), 0o600))
	link := filepath.Join(ws, "test-results", "link.txt")
	require.NoError(t, os.Symlink(secret, link))
	linkDir := filepath.Join(ws, "test-results", "linkdir")
	require.NoError(t, os.Symlink(outside, linkDir))

	uploads, invalid := resolveUploads(ws, "/p", []file{
		{sourcePath: link, destination: "a/link.txt"},
		{sourcePath: filepath.Join(linkDir, "secret.txt"), destination: "a/secret.txt"},
	})
	assert.Empty(t, uploads)
	assert.Len(t, invalid, 2)
}

func TestUploadOneRefusesPathSwappedForSymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("creating symlinks needs elevated rights on Windows")
	}
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	outside := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(outside, "secret.txt"), []byte("s"), 0o600))

	uploads, invalid := resolveUploads(ws, "/p", []file{
		{sourcePath: filepath.Join(ws, "test-results", "home-chromium", "video.webm"), destination: "home-chromium/video.webm"},
	})
	require.Empty(t, invalid)
	require.Len(t, uploads, 1)

	// The step swaps the directory for a symlink after the check.
	dir := filepath.Join(ws, "test-results", "home-chromium")
	require.NoError(t, os.RemoveAll(dir))
	require.NoError(t, os.Symlink(outside, dir))
	require.NoError(t, os.WriteFile(filepath.Join(outside, "video.webm"), []byte("leak"), 0o600))

	err := uploadOne(context.Background(), &Config{URL: srv.URL, AccountID: "a", Registry: "r", Token: "t"}, uploads[0])
	require.Error(t, err)
	assert.Empty(t, rec.paths(), "nothing is sent for a path that now leads out of the workspace")
}

func TestSanitizeErrorMasksToken(t *testing.T) {
	assert.Equal(t, os.ErrClosed, sanitizeError(os.ErrClosed, "tok"))
	masked := sanitizeError(errors.New("Put https://h/f?x=tok123: connection reset"), "tok123")
	assert.Equal(t, "Put https://h/f?x=***: connection reset", masked.Error())
}

func TestUploadOneDoesNotFollowRedirects(t *testing.T) {
	target, rec := newServer(t, nil)
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+r.URL.Path, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(redirector.Close)
	ws := newWorkspace(t)

	uploads, invalid := resolveUploads(ws, "/p", []file{
		{sourcePath: filepath.Join(ws, "test-results", "home-chromium", "video.webm"), destination: "home-chromium/video.webm"},
	})
	require.Empty(t, invalid)
	require.Len(t, uploads, 1)

	err := uploadOne(context.Background(), &Config{URL: redirector.URL, AccountID: "a", Registry: "r", Token: "t"}, uploads[0])
	require.Error(t, err)
	var permErr *permanentError
	assert.True(t, errors.As(err, &permErr), "a redirect is not retried")
	assert.Empty(t, rec.paths(), "the token and body are not sent to the redirect target")
}
