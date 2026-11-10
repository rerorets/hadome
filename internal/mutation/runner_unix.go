//go:build !windows

package mutation

import (
	"os"
	"os/exec"
	"os/signal"
	"sync"
	"syscall"
)

// processTree is everything one test command starts. The command leads a process group of its own,
// and the processes it starts join that group, so killing the group ends them all: a go test killed
// alone would leave behind the test binaries, compilers and linkers it started, still running.
type processTree struct {
	cmd     *exec.Cmd
	sigs    chan os.Signal
	relayed chan struct{} // closed when relay has returned
	mu      sync.Mutex
	pid     int       // the command's, which is also the group's ID; 0 until it started
	dying   os.Signal // a signal that came to end hadome while the command ran
}

// ownTree makes the command lead a process group of its own when it starts.
//
// Out of the terminal's process group, the command no longer receives the terminal's interrupt
// (Ctrl-C), hangup or a terminate sent to that group. So while the command runs, hadome takes those
// signals itself: it kills the group, then ends itself by the same signal, as it would have without
// a handler. A signal hadome was started ignoring stays ignored.
func ownTree(cmd *exec.Cmd) (*processTree, error) {
	t := &processTree{cmd: cmd, sigs: make(chan os.Signal, 1), relayed: make(chan struct{})}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	// exec calls Cancel only after the command started, so cmd.Process is set.
	cmd.Cancel = func() error {
		killGroup(cmd.Process.Pid)
		return nil
	}
	var sigs []os.Signal
	for _, s := range []os.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		if !signal.Ignored(s) {
			sigs = append(sigs, s)
		}
	}
	signal.Notify(t.sigs, sigs...)
	go t.relay()
	return t, nil
}

// relay ends the tree and then hadome when one of the signals arrives. Before the command started,
// started or end does it instead, so that a command that is starting is not missed.
func (t *processTree) relay() {
	defer close(t.relayed)
	s, ok := <-t.sigs
	if !ok {
		return
	}
	t.mu.Lock()
	t.dying = s
	pid := t.pid
	t.mu.Unlock()
	if pid != 0 {
		killGroup(pid)
		t.raise(s)
	}
}

// started is told that the command started.
func (t *processTree) started() error {
	t.mu.Lock()
	t.pid = t.cmd.Process.Pid
	s := t.dying
	t.mu.Unlock()
	if s != nil {
		killGroup(t.pid)
		t.raise(s)
	}
	return nil
}

// end kills whatever is left of the tree after the command exited. A test command that leaves a
// process running (a server it started, a daemon of the build tool) would otherwise keep it running
// into the next mutant's run, and past hadome.
func (t *processTree) end() {
	t.mu.Lock()
	pid := t.pid
	t.mu.Unlock()
	if pid != 0 {
		killGroup(pid)
	}
	signal.Stop(t.sigs)
	close(t.sigs)
	<-t.relayed
	// relay has returned, so nothing else writes dying or pid any more.
	if t.dying != nil && t.pid == 0 {
		t.raise(t.dying) // the command never started
	}
}

// raise ends hadome by the signal s, now that nothing of the tree is running, and never returns.
// A signal sent to the process may reach it only after kill returns, on another thread; returning
// would let the run go on meanwhile, judge the mutant by the tree it just killed, and even exit
// with a status of its own first. With no channel notified, the runtime ends the process on s,
// so waiting here ends too. end waits for relay, so the run stops while relay waits.
func (t *processTree) raise(s os.Signal) {
	signal.Stop(t.sigs)
	syscall.Kill(syscall.Getpid(), s.(syscall.Signal))
	select {}
}

// killGroup kills every process in the group pgid. A group with nothing left in it is not an error.
func killGroup(pgid int) {
	syscall.Kill(-pgid, syscall.SIGKILL)
}
