//go:build !windows

package mutation

import (
	"fmt"
	"io"
	"os"
	"syscall"
)

// HoldLifeline makes the test binary the only holder of a pipe's writing end, and lets every
// process it starts inherit the reading end, named in lifelineEnv by its descriptor and identity.
// Go closes its other files on exec, the writing end among them, so the pipe reaches its end exactly
// when the binary has ended, however it ended; a stand-in that starts after that finds the end at
// once. It returns what closes the pipe.
func HoldLifeline() func() {
	r, w, err := os.Pipe()
	if err != nil {
		return func() {}
	}
	fd := r.Fd()
	if _, _, errno := syscall.Syscall(syscall.SYS_FCNTL, fd, syscall.F_SETFD, 0); errno != 0 {
		r.Close()
		w.Close()
		return func() {}
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(int(fd), &st); err != nil {
		r.Close()
		w.Close()
		return func() {}
	}
	os.Setenv(lifelineEnv, fmt.Sprintf("%d:%d:%d", fd, st.Dev, st.Ino))
	return func() {
		w.Close()
		r.Close()
	}
}

// watchLifeline ends the stand-in once the test binary that holds the lifeline has ended. The
// descriptor is used only when it is still the pipe the binary made: a process in between may have
// closed it and opened something else under its number. It stays open across exec, so that the
// processes a stand-in starts watch it too.
func watchLifeline(v string) {
	var fd int
	var dev, ino uint64
	if _, err := fmt.Sscanf(v, "%d:%d:%d", &fd, &dev, &ino); err != nil {
		return
	}
	var st syscall.Stat_t
	if err := syscall.Fstat(fd, &st); err != nil || uint64(st.Dev) != dev || uint64(st.Ino) != ino {
		return
	}
	go func() {
		io.Copy(io.Discard, os.NewFile(uintptr(fd), "lifeline"))
		os.Exit(lifelineGone)
	}()
}
