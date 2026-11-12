package engine

import (
	"crypto/sha256"
	"errors"
	"io/fs"
	"sync"
	"testing"
	"testing/fstest"
)

// countingFS is a file system that counts how often each file's content is read.
type countingFS struct {
	fstest.MapFS
	mu    sync.Mutex
	reads map[string]int
}

func newCountingFS(files fstest.MapFS) *countingFS {
	return &countingFS{MapFS: files, reads: map[string]int{}}
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.mu.Lock()
	c.reads[name]++
	c.mu.Unlock()
	return c.MapFS.Open(name)
}

func (c *countingFS) ReadFile(name string) ([]byte, error) {
	c.mu.Lock()
	c.reads[name]++
	c.mu.Unlock()
	return c.MapFS.ReadFile(name)
}

func (c *countingFS) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads[name]
}

// viewpoint: invariant
// target: engine.summedFS.Sum
// Design "Content hashes": a file's hash is its content's SHA-256, worked out from one read of the
// file however often and however concurrently it is asked for; a file that cannot be read gives
// its error every time, from one try. Reading the file through the wrapped version still reads it:
// contents are never kept.
func TestAFileIsHashedFromOneRead(t *testing.T) {
	t.Parallel()
	inner := newCountingFS(fstest.MapFS{"a.txt": {Data: []byte("rows\n")}})
	s := newSummedFS(inner)
	want := sha256.Sum256([]byte("rows\n"))

	var wg sync.WaitGroup
	sums := make([][32]byte, 8)
	errs := make([]error, 8)
	for i := range sums {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sums[i], errs[i] = s.Sum("a.txt")
		}()
	}
	wg.Wait()
	for i := range sums {
		if errs[i] != nil || sums[i] != want {
			t.Errorf("call %d: got %x, %v, want %x", i, sums[i], errs[i], want)
		}
	}
	if got, err := s.Sum("a.txt"); err != nil || got != want {
		t.Errorf("a later call: got %x, %v, want %x", got, err, want)
	}
	if n := inner.count("a.txt"); n != 1 {
		t.Errorf("a.txt was read %d times for its hash, want 1", n)
	}

	_, first := s.Sum("missing.txt")
	_, second := s.Sum("missing.txt")
	if !errors.Is(first, fs.ErrNotExist) || !errors.Is(second, fs.ErrNotExist) {
		t.Errorf("a missing file: got %v then %v, want not-exist both times", first, second)
	}
	if n := inner.count("missing.txt"); n != 1 {
		t.Errorf("missing.txt was tried %d times, want 1", n)
	}

	if b, err := fs.ReadFile(s, "a.txt"); err != nil || string(b) != "rows\n" {
		t.Errorf("reading a.txt: got %q, %v", b, err)
	}
	if n := inner.count("a.txt"); n != 2 {
		t.Errorf("after reading a.txt through the version, it was read %d times, want 2", n)
	}
}
