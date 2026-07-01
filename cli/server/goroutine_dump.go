// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package server

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime/pprof"
	"time"
)

const (
	defaultGoroutineDumpDir = "/tmp"
	goroutineDumpDirPerm    = 0700
	goroutineDumpFilePerm   = 0600
	goroutineDumpDebugLevel = 2
)

func writeGoroutineDump(dir string, now time.Time) (string, error) {
	if dir == "" {
		dir = defaultGoroutineDumpDir
	}
	if err := os.MkdirAll(dir, goroutineDumpDirPerm); err != nil {
		return "", err
	}

	path := filepath.Join(
		dir,
		fmt.Sprintf(
			"lite-engine-goroutine-dump-%d-%s.txt",
			os.Getpid(),
			now.UTC().Format("20060102T150405.000000000Z"),
		),
	)
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, goroutineDumpFilePerm)
	if err != nil {
		return "", err
	}
	defer file.Close() //nolint:errcheck

	profile := pprof.Lookup("goroutine")
	if profile == nil {
		return "", errors.New("goroutine profile is not available")
	}
	if err := profile.WriteTo(file, goroutineDumpDebugLevel); err != nil {
		return "", err
	}

	return path, nil
}
