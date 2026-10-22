// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build unix

package attachments

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/harness/lite-engine/api"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A step can replace a planned file with a named pipe. Opening it for reading blocks until a writer shows up, which
// the upload deadline cannot interrupt, so the upload has to refuse it without waiting.
func TestUploadOneDoesNotBlockOnANamedPipe(t *testing.T) {
	srv, rec := newServer(t, nil)
	ws := newWorkspace(t)
	video := filepath.Join(ws, "test-results", "home-chromium", "video.webm")
	uploads, invalid := resolveUploads(ws, "/p", []file{{sourcePath: video, destination: homeVideo}})
	require.Empty(t, invalid)
	require.Len(t, uploads, 1)

	require.NoError(t, os.Remove(video))
	require.NoError(t, syscall.Mkfifo(video, 0o600))

	done := make(chan error, 1)
	go func() {
		done <- uploadOne(context.Background(), &api.HarConfig{URL: srv.URL, AccountID: "a", Registry: "r", Token: "t"}, uploads[0])
	}()
	select {
	case err := <-done:
		var permErr *permanentError
		require.ErrorAs(t, err, &permErr)
		assert.Contains(t, err.Error(), "not a regular file")
	case <-time.After(5 * time.Second):
		t.Fatal("uploadOne blocked on a named pipe")
	}
	assert.Empty(t, rec.paths())
}
