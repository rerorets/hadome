package mutation

import (
	"context"
	"os"
	"strconv"
	"strings"
	"testing"
)

// viewpoint: invariant
// target: mutation.runTree
// Invariant 11: a run that is ended while its test command hangs leaves nothing the command
// started running, as the test binary a go test killed for its time leaves when only go is killed.
func TestAnEndedRunLeavesNothingItsCommandStartedRunning(t *testing.T) {
	t.Parallel()
	if passed := runLeavingALeaf(t, "hang"); passed {
		t.Error("an ended run passed")
	}
}

// viewpoint: invariant
// target: mutation.runTree
// Invariant 11: a test command that exits and leaves a process of its own running does not leave
// it running past the run: it would keep running into the next mutant's run.
func TestARunEndsWhatItsCommandLeftRunning(t *testing.T) {
	t.Parallel()
	if passed := runLeavingALeaf(t, "exit"); !passed {
		t.Error("a command that exited 0 did not pass")
	}
}

// runLeavingALeaf runs StandInTree in mode through runTree, and returns once the leaf has ended.
//
// With mode hang, it ends the run once the leaf is ready. The leaf holds the run's output and keeps
// it full, and the output waits for the leaf to end, so the run cannot finish before. The leaf has
// to end when the run is ended: a leaf ended only once the run has finished is still running when
// the run gives up on its output, and says so.
func runLeavingALeaf(t *testing.T, mode string) bool {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	w := watchLeaf(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	pid := 0
	var endErr error
	out := &lines{line: func(l string) {
		n, ok := strings.CutPrefix(l, "leaf ")
		if !ok {
			return
		}
		pid, _ = strconv.Atoi(n)
		w.seen(pid)
		if mode == "hang" {
			cancel()
			endErr = w.gone()
		}
	}}
	hold := "-"
	if mode == "hang" {
		hold = "hold"
	}
	passed, err := runTree(ctx, t.TempDir(), []string{exe, StandInTree, mode, w.arg, hold}, out)
	if err != nil {
		t.Fatal(err)
	}
	if pid == 0 {
		t.Fatal("the test command never said its leaf was ready")
	}
	if mode != "hang" {
		endErr = w.ended()
	}
	if endErr != nil {
		t.Fatal(endErr)
	}
	return passed
}
