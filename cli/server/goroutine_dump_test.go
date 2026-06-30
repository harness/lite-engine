// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWriteGoroutineDumpWritesProfile(t *testing.T) {
	dir := t.TempDir()
	now := time.Date(2026, 6, 30, 9, 45, 30, 0, time.UTC)

	path, err := writeGoroutineDump(dir, now)

	require.NoError(t, err)
	assert.Equal(t, filepath.Join(dir, "lite-engine-goroutine-dump-"+strconv.Itoa(os.Getpid())+"-20260630T094530.000000000Z.txt"), path)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "TestWriteGoroutineDumpWritesProfile")
}
