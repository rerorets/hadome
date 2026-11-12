package engine

import (
	"crypto/sha256"
	"io/fs"
	"sync"
)

// summedFS is a version's file system that remembers the hash of each file's content: rename
// detection and the analyzer's reuse both hash every file of a version, and no file is read and
// hashed twice for them. It remembers hashes only, never contents, so its memory does not grow with
// the version's size. Every other read goes to the file system it wraps.
//
// Design: docs/design/engine.md, "Content hashes"
type summedFS struct {
	fs.FS
	mu   sync.Mutex
	sums map[string]*fileSum
}

// fileSum is the answer for one file, worked out once.
type fileSum struct {
	once sync.Once
	sum  [32]byte
	err  error
}

func newSummedFS(fsys fs.FS) *summedFS {
	return &summedFS{FS: fsys, sums: map[string]*fileSum{}}
}

// Sum returns the SHA-256 of the content of the file name. Only the first call for a file reads
// it; every later call, a concurrent one too, gets the same answer, an error included. A file that
// changes during the run keeps the hash of its content when it was first read.
func (s *summedFS) Sum(name string) ([32]byte, error) {
	s.mu.Lock()
	f := s.sums[name]
	if f == nil {
		f = &fileSum{}
		s.sums[name] = f
	}
	s.mu.Unlock()
	f.once.Do(func() {
		b, err := fs.ReadFile(s.FS, name)
		if err != nil {
			f.err = err
			return
		}
		f.sum = sha256.Sum256(b)
	})
	return f.sum, f.err
}

// ReadFile, Stat and ReadDir go to the wrapped file system, so that its own ways of serving them
// are kept.
func (s *summedFS) ReadFile(name string) ([]byte, error) { return fs.ReadFile(s.FS, name) }

func (s *summedFS) Stat(name string) (fs.FileInfo, error) { return fs.Stat(s.FS, name) }

func (s *summedFS) ReadDir(name string) ([]fs.DirEntry, error) { return fs.ReadDir(s.FS, name) }
