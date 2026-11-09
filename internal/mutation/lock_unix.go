//go:build !windows

package mutation

import (
	"errors"
	"os"
	"syscall"
)

// lock takes the lock file at path, or fails at once when another process holds it. flock locks
// belong to the open file, so the kernel releases them when the process ends, however it ends.
func lock(path string) (release func(), err error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o644)
	if err != nil {
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, held(path)
		}
		return nil, err
	}
	if err := writePid(f); err != nil {
		f.Close()
		return nil, err
	}
	return func() { f.Close() }, nil
}
