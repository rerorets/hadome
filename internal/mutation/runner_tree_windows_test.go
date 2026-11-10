package mutation

import (
	"errors"
	"fmt"
	"os"
	"sync/atomic"
	"syscall"
	"testing"
	"unsafe"
)

// leafWatch learns that a leaf ended by waiting on a handle to it, opened while the leaf is known to
// run, so that its process ID cannot be reused in between. A leaf that is asked on its named pipe,
// or that finds the run's output given up on, exits by itself with leafAlive, which a leaf the job
// terminated never does: so a leaf left running is told apart at once instead of being waited for.
type leafWatch struct {
	t   *testing.T
	arg string // the name of the leaf's pipe
	h   syscall.Handle
	err error
}

// leafAlive is the exit code of a leaf that was still running when it was asked.
const leafAlive = 3

var (
	procCreateNamedPipeW = kernel32.NewProc("CreateNamedPipeW")
	procConnectNamedPipe = kernel32.NewProc("ConnectNamedPipe")
)

const (
	pipeAccessInbound = 0x00000001
	errPipeConnected  = syscall.Errno(535) // ERROR_PIPE_CONNECTED: the client came before the wait
)

// leaves numbers the pipes of this test binary, which tests running at once share.
var leaves atomic.Int64

func watchLeaf(t *testing.T) *leafWatch {
	return &leafWatch{t: t, arg: fmt.Sprintf(`\\.\pipe\hadome-leaf-%d-%d`, os.Getpid(), leaves.Add(1))}
}

// seen is told the leaf's PID while the leaf runs. When the test ends, the leaf is terminated if it
// still runs: code under test that fails to end it fails the test, and must not leave it running
// past it. The handle names that process alone, even once its ID is reused.
func (w *leafWatch) seen(pid int) {
	w.h, w.err = syscall.OpenProcess(syscall.SYNCHRONIZE|syscall.PROCESS_QUERY_INFORMATION, false, uint32(pid))
	if h, err := syscall.OpenProcess(syscall.PROCESS_TERMINATE, false, uint32(pid)); err == nil {
		w.t.Cleanup(func() {
			syscall.TerminateProcess(h, 1)
			syscall.CloseHandle(h)
		})
	}
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

// ended asks the leaf on its pipe whether it is still running, then waits like gone. A leaf being
// terminated runs no more of its own code and never answers; a pipe that cannot be opened has no
// leaf behind it.
func (w *leafWatch) ended() error {
	if p, err := os.OpenFile(w.arg, os.O_WRONLY, 0); err == nil {
		p.Write([]byte{'?'})
		p.Close()
	}
	return w.gone()
}

// StandInLeafRun is the process of StandInLeaf. It waits on the named pipe WATCH, says "ready" and
// blocks forever. Asked on the pipe, it exits with leafAlive. With HOLD "hold", it keeps writing to
// its standard error, the run's output, and exits with leafAlive when that fails.
func StandInLeafRun(watch, hold string) int {
	name, err := syscall.UTF16PtrFromString(watch)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	r, _, err := procCreateNamedPipeW.Call(uintptr(unsafe.Pointer(name)), pipeAccessInbound, 0, 1, 0, 64, 0, 0)
	if syscall.Handle(r) == syscall.InvalidHandle {
		fmt.Println("creating the pipe:", err)
		return 2
	}
	pipe := syscall.Handle(r)
	go func() {
		if ok, _, err := procConnectNamedPipe.Call(uintptr(pipe), 0); ok == 0 && !errors.Is(err, errPipeConnected) {
			return
		}
		var n uint32
		syscall.ReadFile(pipe, make([]byte, 1), &n, nil)
		os.Exit(leafAlive)
	}()
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
