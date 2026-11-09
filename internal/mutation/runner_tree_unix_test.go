//go:build !windows

package mutation

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// leafWatch learns that a leaf ended from the FIFO the leaf holds open for writing: the FIFO reaches
// its end when its only writer is gone, which a PID that lingers as a zombie cannot tell.
type leafWatch struct {
	arg  string
	dead chan struct{}
}

func watchLeaf(t *testing.T) *leafWatch {
	t.Helper()
	fifo := filepath.Join(t.TempDir(), "leaf")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	w := &leafWatch{arg: fifo, dead: make(chan struct{})}
	go func() {
		defer close(w.dead)
		f, err := os.Open(fifo) // blocks until the leaf opens it for writing
		if err != nil {
			return
		}
		defer f.Close()
		io.Copy(io.Discard, f)
	}()
	return w
}

func (w *leafWatch) seen(int) {}

// gone waits until the leaf has ended.
func (w *leafWatch) gone() error {
	<-w.dead
	return nil
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
			pgid, err := syscall.Getpgid(pid)
			switch {
			case err != nil:
				group = err
			case pgid == syscall.Getpgrp():
				group = fmt.Errorf("the leaf is in hadome's process group %d", pgid)
			}
			cancel()
		}}
		if _, err := runTree(ctx, t.TempDir(), []string{exe, StandInTree, "hang", w.arg}, out); err != nil {
			t.Fatal(err)
		}
		if group != nil {
			t.Error(group)
		}
		w.gone()
	})
	for _, sig := range []syscall.Signal{syscall.SIGINT, syscall.SIGTERM, syscall.SIGHUP} {
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
			if !strings.HasPrefix(line, "leaf ") {
				cmd.Process.Kill()
				cmd.Wait()
				t.Fatalf("the test command did not say its leaf was ready: %q", line)
			}
			cmd.Process.Signal(sig)
			w.gone()
			err = cmd.Wait()
			ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
			if !ok || !ws.Signaled() || ws.Signal() != sig {
				t.Errorf("want hadome ended by %v, got %v", sig, err)
			}
		})
	}
}
