package filesystem

import (
	"io"
	"os"
)

//go:generate mockgen -source filesystem.go -destination mock_filesystem.go -package filesystem FileSystem File
type FileSystem interface {
	Open(name string) (File, error)
	Stat(name string) (os.FileInfo, error)
	ReadFile(filename string, op func(io.Reader) error) error
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

func (*osFS) Open(name string) (File, error)        { return os.Open(name) }
func (*osFS) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
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
