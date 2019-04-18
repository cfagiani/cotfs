package storage

import (
	"io"
	"os"
)

// Abstraction over the file storage system.
type FileStorage interface {
	Open(name string) (File, error)
	Stat(name string) (os.FileInfo, error)
}

type File interface {
	io.Closer
	io.Reader
	//io.ReaderAt
	//io.Seeker
	Stat() (os.FileInfo, error)
}

// LocalFileStorage implements the fileStorage interface using the local OS
type LocalFileStorage struct{}

// Opens a local file by delegating to the os.Open function
func (LocalFileStorage) Open(name string) (File, error) { return os.Open(name) }

// Stats a local file by delegating to the os.Stat function
func (LocalFileStorage) Stat(name string) (os.FileInfo, error) { return os.Stat(name) }
