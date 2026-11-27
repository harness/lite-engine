// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build unix

package runtime

import (
	"errors"
	"os"
	"syscall"
)

// openRegularNoFollow opens path for reading without following a final symlink
// and returns the file only if the opened descriptor is a regular file.
// O_NONBLOCK keeps the open of a named pipe from blocking until a writer appears;
// the regular-file check then rejects it.
func openRegularNoFollow(path string) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() {
		f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}
