//go:build !windows

package mutation

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// leafWatch talks with a leaf through two FIFOs in one directory. The leaf is the only writer of
// out, so out reaches its end when the leaf has ended, which a PID that lingers as a zombie cannot
// tell. A leaf that is asked on ping, or that finds the run's output given up on, writes "alive"
// to out and exits: so a leaf left running is told apart at once instead of being waited for.
type leafWatch struct {
	t   *testing.T
	arg string
	out *os.File
}

func watchLeaf(t *testing.T) *leafWatch {
	t.Helper()
	dir := t.TempDir()
	for _, name := range []string{"out", "ping"} {
		if err := syscall.Mkfifo(filepath.Join(dir, name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	// Opened without waiting for a writer, so that neither end of out waits for the other, then
	// read blocking: not every system polls FIFOs.
	path := filepath.Join(dir, "out")
	fd, err := syscall.Open(path, syscall.O_RDONLY|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := syscall.SetNonblock(fd, false); err != nil {
		syscall.Close(fd)
		t.Fatal(err)
	}
	out := os.NewFile(uintptr(fd), path)
	t.Cleanup(func() { out.Close() })
	return &leafWatch{t: t, arg: dir, out: out}
}

// seen is told the leaf's PID while the leaf runs. When the test ends, whatever is left of the
// leaf's process group, the command's tree with its root, is killed: code under test that fails to
// end the tree fails the test, and must not leave the tree running past it. The test's own group is
// never killed.
func (w *leafWatch) seen(pid int) {
	pgid, err := syscall.Getpgid(pid)
	if err != nil || pgid == syscall.Getpgrp() {
		return
	}
	w.t.Cleanup(func() { syscall.Kill(-pgid, syscall.SIGKILL) })
}

// gone waits until the leaf has ended, after it said it was ready, and fails if it said it was
// still running.
func (w *leafWatch) gone() error {
	said, err := io.ReadAll(w.out)
	if err != nil {
		return err
	}
	if bytes.Contains(said, []byte("alive")) {
		return errors.New("the leaf was still running")
	}
	return nil
}

// ended asks the leaf whether it is still running, then waits like gone. A leaf that is ending
// has its kill pending and never answers.
func (w *leafWatch) ended() error {
	ping, err := os.OpenFile(filepath.Join(w.arg, "ping"), os.O_WRONLY|syscall.O_NONBLOCK, 0)
	if err == nil { // ENXIO: no one has ping open for reading, so the leaf is gone
		ping.Write([]byte{'?'})
		ping.Close()
	}
	return w.gone()
}

// StandInLeafRun is the process of StandInLeaf. It opens the FIFOs of WATCH, says "ready" and
// blocks forever, holding the run's output open when HOLD is "open". Asked on ping, it says "alive"
// on out and exits. With HOLD "hold", it keeps
// writing to its standard error, the run's output, and says "alive" and exits when that fails.
func StandInLeafRun(watch, hold string) int {
	out, err := os.OpenFile(filepath.Join(watch, "out"), os.O_WRONLY, 0)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	// Holding ping for writing too, its reads wait for data instead of finding no writer.
	ping, err := os.OpenFile(filepath.Join(watch, "ping"), os.O_RDWR, 0)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	alive := func() {
		fmt.Fprintln(out, "alive")
		os.Exit(3)
	}
	go func() {
		ping.Read(make([]byte, 1))
		alive()
	}()
	fmt.Println("ready")
	if hold == "hold" {
		// A copy of standard error, so that a failed write is an error and not SIGPIPE.
		fd, err := syscall.Dup(2)
		if err != nil {
			alive()
		}
		f := os.NewFile(uintptr(fd), "output")
		newlines := bytes.Repeat([]byte{'\n'}, 1<<16)
		for {
			if _, err := f.Write(newlines); err != nil {
				alive()
			}
		}
	}
	blockForever()
	return 0
}

// viewpoint: invariant
// target: mutation.runTree
// Invariant 11: the test command and what it starts run out of hadome's process group, and so out
// of the terminal's, so an interrupt such as Ctrl-C reaches hadome alone. hadome ends the
// command's tree, then ends by that same signal.
func TestAnInterruptedRunEndsItsTreeThenHadome(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Run("out of the group", func(t *testing.T) {
		t.Parallel()
		w := watchLeaf(t)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		var group error
		out := &lines{line: func(l string) {
			n, ok := strings.CutPrefix(l, "leaf ")
			if !ok {
				return
			}
			pid, _ := strconv.Atoi(n)
			w.seen(pid)
			pgid, err := syscall.Getpgid(pid)
			switch {
			case err != nil:
				group = err
			case pgid == syscall.Getpgrp():
				group = fmt.Errorf("the leaf is in hadome's process group %d", pgid)
			}
			cancel()
		}}
		if _, err := runTree(ctx, t.TempDir(), []string{exe, StandInTree, "hang", w.arg, "-"}, out); err != nil {
			t.Fatal(err)
		}
		if group != nil {
			t.Error(group)
		}
		if err := w.ended(); err != nil {
			t.Error(err)
		}
	})
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
		// A signal ignored here, as SIGHUP under nohup, would be ignored by the stand-in too, which
		// keeps it ignored as hadome does. Catching it here lets the stand-in start with it unignored.
		if signal.Ignored(sig) {
			signal.Notify(make(chan os.Signal, 1), sig)
		}
		t.Run(sig.String(), func(t *testing.T) {
			t.Parallel()
			w := watchLeaf(t)
			cmd := exec.Command(exe, StandInRunTree, w.arg)
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatal(err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			line, _ := bufio.NewReader(out).ReadString('\n')
			n, ok := strings.CutPrefix(strings.TrimSpace(line), "leaf ")
			if !ok {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("the test command did not say its leaf was ready: %q", line)
			}
			pid, _ := strconv.Atoi(n)
			w.seen(pid)
			cmd.Process.Signal(sig)
			err = cmd.Wait()
			if err := w.ended(); err != nil {
				t.Error(err)
			}
			ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !ws.Signaled() || ws.Signal() != sig {
				t.Errorf("want hadome ended by %v, got %v", sig, err)
			}
		})
	}
}

// StandInEarlySignalRun is the hadome mutate of StandInEarlySignal. The interrupt goes straight to
// the tree's channel, as signal.Notify would deliver it, and has been taken in once relay returns.
func StandInEarlySignalRun(mode, watch string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	// A context, as runTree gives, without which a command that has Cancel set cannot start.
	cmd := exec.CommandContext(context.Background(), exe, StandInLeaf, watch, "-")
	if mode == "unstarted" {
		cmd = exec.CommandContext(context.Background(), filepath.Join(watch, "no such command"))
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	tree, err := ownTree(cmd)
	if err != nil {
		fmt.Println(err)
		return 2
	}
	tree.sigs <- syscall.SIGINT
	<-tree.relayed
	defer fmt.Println("no signal")
	defer tree.end()
	if err := cmd.Start(); err != nil {
		fmt.Println(err)
		return 2
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		fmt.Printf("the leaf said %q\n", line)
		return 2
	}
	tree.started()
	return 2
}

// StandInAfterRunRun is the hadome mutate of StandInAfterRun.
func StandInAfterRunRun() int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	dir, err := os.MkdirTemp("", "stand-in-after-run")
	if err != nil {
		fmt.Println(err)
		return 2
	}
	defer os.RemoveAll(dir)
	if passed, err := runTree(context.Background(), dir, []string{exe, "-test.run=^$"}, io.Discard); !passed || err != nil {
		fmt.Println("the command did not pass:", err)
		return 2
	}
	// Nothing is notified of it any more, so its default action ends the process.
	syscall.Kill(syscall.Getpid(), syscall.SIGINT)
	blockForever()
	return 2
}

// viewpoint: invariant
// target: mutation.processTree
// Invariant 11: an interrupt that arrives while the test command is being started ends what the
// command started as soon as it has started, then hadome by that signal; one that arrives when the
// command never starts ends hadome by that signal all the same.
func TestAnInterruptBeforeTheCommandStartedEndsItsTreeThenHadome(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if signal.Ignored(syscall.SIGINT) { // as under nohup; the stand-in would keep it ignored
		signal.Notify(make(chan os.Signal, 1), syscall.SIGINT)
	}
	for _, mode := range []string{"started", "unstarted"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			w := watchLeaf(t)
			cmd := exec.Command(exe, StandInEarlySignal, mode, w.arg)
			said, _ := cmd.Output()
			if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
				t.Errorf("want hadome ended by an interrupt, got %v saying %q", cmd.ProcessState, said)
			}
			if mode == "started" {
				if err := w.ended(); err != nil {
					t.Error(err)
				}
			}
		})
	}
}

// viewpoint: invariant
// target: mutation.processTree.end
// Once a run has ended, hadome takes interrupts as it did before the run: it ends by them, and
// nothing of the run is told of them any more.
func TestAnInterruptAfterARunEndsHadome(t *testing.T) {
	t.Parallel()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if signal.Ignored(syscall.SIGINT) {
		signal.Notify(make(chan os.Signal, 1), syscall.SIGINT)
	}
	cmd := exec.Command(exe, StandInAfterRun)
	said, _ := cmd.CombinedOutput()
	if ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus); !ok || !ws.Signaled() || ws.Signal() != syscall.SIGINT {
		t.Errorf("want hadome ended by an interrupt, got %v saying %q", cmd.ProcessState, said)
	}
}
