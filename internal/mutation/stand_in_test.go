package mutation

import (
	"bufio"
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strings"
	"testing"
	"time"
)

// The test binary stands in for the processes this package's tests need; TestMain in
// mutation_test.go dispatches to these when its first argument names one.
const (
	// StandInStages makes the binary a test command that records the stages it runs in:
	// `<binary> stand-in-stages LOG SPEC ARG...`. It appends ARG... and the last line of x.go and y.py
	// to LOG. SPEC is a comma-separated list of ACTION=ARGS, ARGS being ARG... joined by spaces:
	// fail fails always, and, when x.go is broken (holds BROKEN), hang never ends and catch fails.
	StandInStages = "stand-in-stages"
	// StandInHoldLock makes the binary a second hadome mutate: `<binary> stand-in-hold-lock PATH`
	// takes the lock at PATH, says "held" and holds it until its standard input closes or it is
	// killed.
	StandInHoldLock = "stand-in-hold-lock"
	// StandInUnwritableLock makes the binary a hadome mutate that cannot write files:
	// `<binary> stand-in-unwritable-lock PATH` tries the lock at PATH twice and prints each
	// attempt's error, or "taken". No test starts it on Windows.
	StandInUnwritableLock = "stand-in-unwritable-lock"
	// StandInTree makes the binary a test command that starts a process of its own:
	// `<binary> stand-in-tree MODE WATCH HOLD` starts StandInLeaf with WATCH and HOLD, its standard
	// error being the command's own when HOLD is "hold", waits until it is ready and says
	// "leaf PID". Then, with MODE hang, it blocks forever; with MODE exit, it exits 0, leaving the
	// leaf running.
	StandInTree = "stand-in-tree"
	// StandInLeaf makes the binary the process StandInTree starts: `<binary> stand-in-leaf WATCH
	// HOLD` says "ready" and blocks forever (StandInLeafRun says how on each platform).
	StandInLeaf = "stand-in-leaf"
	// StandInRunTree makes the binary a hadome mutate running a test command:
	// `<binary> stand-in-run-tree WATCH` runs StandInTree in mode hang with WATCH, its output going
	// to standard output, until the binary is ended by a signal. No test starts it on Windows.
	StandInRunTree = "stand-in-run-tree"
	// StandInHolder makes the binary a test binary that runs a test command: `<binary>
	// stand-in-holder WATCH` holds a lifeline of its own (HoldLifeline), starts StandInTree in mode
	// hang with WATCH, passes on what it says and blocks until it is killed.
	StandInHolder = "stand-in-holder"
)

// StandInStage is the test command of StandInStages.
func StandInStage(args []string) int {
	log, spec, rest := args[0], args[1], args[2:]
	joined := strings.Join(rest, " ")
	x, _ := os.ReadFile("x.go")
	y, _ := os.ReadFile("y.py")
	f, err := os.OpenFile(log, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644)
	if err != nil {
		return 2
	}
	fmt.Fprintf(f, "%s | %s | %s\n", joined, lastLine(x), lastLine(y))
	f.Close()
	broken := strings.Contains(string(x), "BROKEN")
	for _, rule := range strings.Split(spec, ",") {
		action, on, _ := strings.Cut(rule, "=")
		if on != joined {
			continue
		}
		switch {
		case action == "fail":
			return 1
		case action == "hang" && broken:
			time.Sleep(time.Hour) // the runner's limit ends it
		case action == "catch" && broken:
			return 1
		}
	}
	return 0
}

// StandInHold is the second hadome mutate of StandInHoldLock.
func StandInHold(path string) int {
	if _, err := lock(path); err != nil {
		fmt.Println(err)
		return 1
	}
	fmt.Println("held")
	io.Copy(io.Discard, os.Stdin)
	return 0
}

// StandInTreeRoot is the test command of StandInTree.
func StandInTreeRoot(mode, watch, hold string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	leaf := exec.Command(exe, StandInLeaf, watch, hold)
	if hold == "hold" {
		leaf.Stderr = os.Stderr
	}
	out, err := leaf.StdoutPipe()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	if err := leaf.Start(); err != nil {
		fmt.Println(err)
		return 2
	}
	if line, _ := bufio.NewReader(out).ReadString('\n'); line != "ready\n" {
		fmt.Printf("the leaf said %q\n", line)
		return 2
	}
	fmt.Printf("leaf %d\n", leaf.Process.Pid)
	if mode == "hang" {
		blockForever()
	}
	return 0
}

// StandInRunTreeRun is the hadome mutate of StandInRunTree.
func StandInRunTreeRun(watch string) int {
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	dir, err := os.MkdirTemp("", "stand-in-run-tree")
	if err != nil {
		fmt.Println(err)
		return 2
	}
	defer os.RemoveAll(dir)
	_, err = runTree(context.Background(), dir, []string{exe, StandInTree, "hang", watch, "-"}, os.Stdout)
	fmt.Println("the run ended without a signal:", err)
	return 2
}

// StandInHolderRun is the test binary of StandInHolder.
func StandInHolderRun(watch string) int {
	HoldLifeline()
	exe, err := os.Executable()
	if err != nil {
		fmt.Println(err)
		return 2
	}
	tree := exec.Command(exe, StandInTree, "hang", watch, "-")
	tree.Stdout = os.Stdout
	if err := tree.Start(); err != nil {
		fmt.Println(err)
		return 2
	}
	blockForever()
	return 0
}

// blockForever reads a pipe no one writes to. It holds the pipe's writing end itself, so the read
// never reaches the end.
func blockForever() {
	r, w, err := os.Pipe()
	if err != nil {
		os.Exit(2)
	}
	r.Read(make([]byte, 1))
	w.Close()
}

// lines is a writer that hands each complete line written to it, without its newline, to line.
type lines struct {
	line func(string)
	buf  []byte
}

func (l *lines) Write(p []byte) (int, error) {
	l.buf = append(l.buf, p...)
	for {
		i := slices.Index(l.buf, '\n')
		if i < 0 {
			return len(p), nil
		}
		l.line(string(l.buf[:i]))
		l.buf = l.buf[i+1:]
	}
}

// WithoutBaselineMark is a file's contents without the comment a run before breaking adds at its end.
func WithoutBaselineMark(s string) string {
	for _, c := range []string{"//", "#"} {
		if i := strings.LastIndex(s, "\n"+c+" hadome mutate baseline "); i >= 0 {
			return s[:i+1]
		}
	}
	return s
}

func lastLine(b []byte) string {
	lines := strings.Split(strings.TrimRight(string(b), "\n"), "\n")
	return lines[len(lines)-1]
}

// standInLines reads what the stand-in test commands logged.
func standInLines(t *testing.T, log string) []string {
	t.Helper()
	f, err := os.Open(log)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []string
	s := bufio.NewScanner(f)
	for s.Scan() {
		out = append(out, s.Text())
	}
	return out
}

// lifelineEnv names, in the environment every stand-in inherits, how to tell that the test binary
// running the tests has ended (HoldLifeline). A stand-in left running by code under test, or by a
// test binary killed before its cleanups ran, then ends with it instead of running on.
const lifelineEnv = "HADOME_TEST_LIFELINE"

// lifelineGone is the exit code of a stand-in that ended because the test binary had ended.
const lifelineGone = 4

// WatchLifeline makes a stand-in end once the test binary that started it, directly or through
// other stand-ins, has ended. It never ends a stand-in while the binary runs, so a stand-in that
// code under test should have ended still shows that it was not.
func WatchLifeline() {
	if v := os.Getenv(lifelineEnv); v != "" {
		watchLifeline(v)
	}
}
