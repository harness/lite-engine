// Copyright 2026 Harness Inc. All rights reserved.
// Use of this source code is governed by the PolyForm Free Trial 1.0.0 license
// that can be found in the licenses directory at the root of this repository, also available at
// https://polyformproject.org/wp-content/uploads/2020/05/PolyForm-Free-Trial-1.0.0.txt.

//go:build !unix

package runtime

import (
	"errors"
	"os"
)

// openRegularNoFollow opens path for reading and returns the file only if it is
// the regular file found at path before the open. There is no O_NOFOLLOW here,
// so a symlink swapped in after the Lstat opens a different file and is
// rejected by SameFile.
func openRegularNoFollow(path string) (*os.File, error) {
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, err
	}
	if !info.Mode().IsRegular() || !os.SameFile(before, info) {
		f.Close()
		return nil, errors.New("not a regular file")
	}
	return f, nil
}
