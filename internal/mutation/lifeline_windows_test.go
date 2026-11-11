package mutation

import (
	"os"
	"strconv"
	"syscall"
)

// HoldLifeline puts the test binary's process ID where its stand-ins find it, in lifelineEnv. A
// stand-in waits on a handle to that process, which ends when the binary has ended, however it
// ended.
func HoldLifeline() func() {
	os.Setenv(lifelineEnv, strconv.Itoa(os.Getpid()))
	return func() {}
}

// watchLifeline ends the stand-in once the test binary has ended. A binary that cannot be opened
// has already ended.
func watchLifeline(pid string) {
	n, err := strconv.Atoi(pid)
	if err != nil {
		os.Exit(lifelineGone)
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(n))
	if err != nil {
		os.Exit(lifelineGone)
	}
	go func() {
		syscall.WaitForSingleObject(h, syscall.INFINITE)
		os.Exit(lifelineGone)
	}()
}
