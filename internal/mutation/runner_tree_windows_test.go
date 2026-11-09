package mutation

import (
	"fmt"
	"syscall"
	"testing"
)

// leafWatch learns that a leaf ended by waiting on a handle to it, opened while the leaf is known to
// run, so that its process ID cannot be reused in between.
type leafWatch struct {
	arg string
	h   syscall.Handle
	err error
}

func watchLeaf(*testing.T) *leafWatch { return &leafWatch{arg: "-"} }

func (w *leafWatch) seen(pid int) {
	w.h, w.err = syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
}

// gone waits until the leaf has ended.
func (w *leafWatch) gone() error {
	if w.err != nil {
		return fmt.Errorf("opening the leaf: %w", w.err)
	}
	defer syscall.CloseHandle(w.h)
	if _, err := syscall.WaitForSingleObject(w.h, syscall.INFINITE); err != nil {
		return fmt.Errorf("waiting for the leaf: %w", err)
	}
	return nil
}
