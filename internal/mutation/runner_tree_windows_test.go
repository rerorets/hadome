package mutation

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"testing"
)

// leafWatch learns that a leaf ended by waiting on a handle to it, opened while the leaf is known to
// run, so that its process ID cannot be reused in between. A leaf that finds the run's output given
// up on exits by itself with leafAlive, which a leaf the job terminated never does.
type leafWatch struct {
	arg string
	h   syscall.Handle
	err error
}

// leafAlive is the exit code of a leaf that was still running when the run gave up on its output.
const leafAlive = 3

func watchLeaf(*testing.T) *leafWatch { return &leafWatch{arg: "-"} }

func (w *leafWatch) seen(pid int) {
	w.h, w.err = syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
}

// gone waits until the leaf has ended, and fails if it ended by itself.
func (w *leafWatch) gone() error {
	if w.err != nil {
		return fmt.Errorf("opening the leaf: %w", w.err)
	}
	defer syscall.CloseHandle(w.h)
	if _, err := syscall.WaitForSingleObject(w.h, syscall.INFINITE); err != nil {
		return fmt.Errorf("waiting for the leaf: %w", err)
	}
	var code uint32
	if err := syscall.GetExitCodeProcess(w.h, &code); err != nil {
		return fmt.Errorf("reading how the leaf ended: %w", err)
	}
	if code == leafAlive {
		return errors.New("the leaf was still running")
	}
	return nil
}

// ended waits like gone.
func (w *leafWatch) ended() error { return w.gone() }

// StandInLeafRun is the process of StandInLeaf. It says "ready" and blocks forever. With HOLD
// "hold", it keeps writing to its standard error, the run's output, and exits with leafAlive when
// that fails.
func StandInLeafRun(_, hold string) int {
	fmt.Println("ready")
	if hold == "hold" {
		newlines := make([]byte, 1<<16)
		for i := range newlines {
			newlines[i] = '\n'
		}
		for {
			if _, err := os.Stderr.Write(newlines); err != nil {
				return leafAlive
			}
		}
	}
	blockForever()
	return 0
}
