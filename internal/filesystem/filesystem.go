// Copyright 2022 Drone.IO Inc. All rights reserved.
// Use of this source code is governed by the Polyform License
// that can be found in the LICENSE file.

package filesystem

import (
	"io"
	"os"
)

//go:generate mockgen -source filesystem.go -destination mock_filesystem.go -package filesystem FileSystem File
type FileSystem interface {
	Open(name string) (File, error)
	Stat(name string) (os.FileInfo, error)
	Remove(name string) error
	ReadFile(filename string, op func(io.Reader) error) error
	MkdirAll(path string, perm os.FileMode) error
	Create(name string) (*os.File, error)
}

type File interface {
	io.Closer
	io.Reader
	io.ReaderAt
	io.Seeker
	Stat() (os.FileInfo, error)
}

// osFS implements fileSystem using the local disk.
type osFS struct{}

func (*osFS) Open(name string) (File, error)               { return os.Open(name) }
func (*osFS) Stat(name string) (os.FileInfo, error)        { return os.Stat(name) }
func (*osFS) MkdirAll(path string, perm os.FileMode) error { return os.MkdirAll(path, perm) }
func (*osFS) Create(name string) (*os.File, error)         { return os.Create(name) }
func (*osFS) Remove(name string) error                     { return os.Remove(name) }

func (*osFS) ReadFile(filename string, op func(io.Reader) error) error {
	f, err := os.Open(filename)
	if err != nil {
		return err
	}
	defer f.Close()
	return op(f)
}

func New() FileSystem {
	return &osFS{}
}
