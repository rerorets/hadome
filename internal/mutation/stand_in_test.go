package mutation

import (
	"bufio"
	"fmt"
	"io"
	"os"
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
