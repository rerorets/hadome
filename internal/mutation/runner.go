package mutation

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// Outcome is what the tests did with one mutant.
type Outcome int

// Outcomes.
const (
	Caught Outcome = iota
	Survived
	// Unbuilt is a mutant in a file the build that runs the tests does not compile. No test is run
	// for it, so it is neither caught nor a survivor.
	Unbuilt
)

// The placeholders of the test command.
const (
	testsHole    = "{tests}"
	packagesHole = config.PackagesPlaceholder
	// allPackages is what {packages} becomes in the stages that run every package.
	allPackages = "./..."
)

// Runner runs the test command in a copy of one version. It never writes to the repository's
// working tree: only the copy is broken, and each mutant is restored before the next.
type Runner struct {
	Test []string // the configured command; an element may hold {tests}, or be {packages}
	Sep  string   // what joins test names in {tests}
	// Timeout is the configured limit of every run. Zero means a limit per command, worked out from
	// that command's run before anything is broken.
	Timeout time.Duration
	dir     string
	// marked are the files holding mutants, with the line comment of their language. Each run before
	// breaking adds a comment to them, so that caches keyed by contents miss there as they do for a
	// mutant, and the run is not measured shorter than a mutant's.
	marked map[string]string
	mark   string // unique to this runner, so that no two runs before breaking add the same comment
	runs   int
	before map[string]baseline // by command
	// skipped is told once of each narrowed command that fails with nothing broken.
	skipped func(cmd []string)
	clock   clock
}

// baseline is what a command did with nothing broken.
type baseline struct {
	passed bool
	limit  time.Duration // of a run of this command with a mutant
	out    string
}

func newRunner(dir string, c config.Mutation, marked map[string]string, skipped func([]string)) (*Runner, error) {
	var timeout time.Duration
	if c.Timeout != "" {
		var err error
		if timeout, err = time.ParseDuration(c.Timeout); err != nil {
			return nil, err
		}
	}
	b := make([]byte, 4)
	if _, err := rand.Read(b); err != nil {
		return nil, err
	}
	return &Runner{Test: c.Test, Sep: c.TestsSeparator, Timeout: timeout, dir: dir, marked: marked, mark: hex.EncodeToString(b),
		before: map[string]baseline{}, skipped: skipped, clock: wallClock}, nil
}

// clean runs every test once on the unbroken copy. Failing tests make every mutant look caught, so
// they stop the run.
func (r *Runner) clean() error {
	b, err := r.baseline(fill(r.Test, r.Sep, nil, nil))
	if err != nil {
		return err
	}
	if !b.passed {
		return errors.New("the tests fail with nothing broken, so no mutant can be judged; fix the tests first:\n" + tail(b.out))
	}
	return nil
}

// try applies one mutant and runs the stages from the narrowest: tests names the tests that call the
// broken function, pkgs the packages they are in (nil when they cannot be narrowed by package). A
// stage that fails catches the mutant; one whose command fails with nothing broken is passed over.
// The file is restored afterwards.
func (r *Runner) try(m facts.Mutant, tests, pkgs []string) (Outcome, error) {
	p := filepath.Join(r.dir, filepath.FromSlash(m.Path))
	orig, err := os.ReadFile(p)
	if err != nil {
		return Caught, err
	}
	broken := append(append(append([]byte{}, orig[:m.Start]...), m.Replacement...), orig[m.End:]...)
	defer os.WriteFile(p, orig, 0o644)
	for _, cmd := range stages(r.Test, r.Sep, tests, pkgs) {
		b, err := r.baseline(cmd)
		if err != nil {
			return Caught, err
		}
		if !b.passed {
			continue
		}
		if err := os.WriteFile(p, broken, 0o644); err != nil {
			return Caught, err
		}
		_, passed, timedOut, err := r.run(cmd, b.limit)
		// A timeout catches the mutant only against a confirmed limit: a run that went past it because
		// the machine got loaded runs again with the limit raised, a bounded number of times.
		for i := 0; err == nil && timedOut && r.Timeout == 0 && i < confirmations; i++ {
			raised, cerr := r.confirm(cmd, p, orig, broken)
			if cerr != nil {
				return Caught, cerr
			}
			if !raised {
				break
			}
			_, passed, timedOut, err = r.run(cmd, r.before[strings.Join(cmd, "\x00")].limit)
		}
		if err != nil || !passed {
			return Caught, err
		}
		// The next stage's run before breaking, when it has not run yet, needs the unbroken file.
		if err := os.WriteFile(p, orig, 0o644); err != nil {
			return Caught, err
		}
	}
	return Survived, nil
}

// baseline runs a command once with nothing broken, the first time it is asked for, with a comment
// added to every file that holds a mutant.
func (r *Runner) baseline(cmd []string) (baseline, error) {
	key := strings.Join(cmd, "\x00")
	if b, ok := r.before[key]; ok {
		return b, nil
	}
	m, err := r.measure(cmd)
	if err != nil {
		return baseline{}, err
	}
	b := baseline{passed: m.passed, limit: r.Timeout, out: m.out}
	if b.limit == 0 {
		b.limit = limitAfter(m.took)
	}
	r.before[key] = b
	if !b.passed && r.skipped != nil && key != strings.Join(fill(r.Test, r.Sep, nil, nil), "\x00") {
		r.skipped(cmd)
	}
	return b, nil
}

// addMarks appends a comment unique to this run to every file holding a mutant, and returns what
// takes them out again.
func (r *Runner) addMarks() (func() error, error) {
	r.runs++
	origs := map[string][]byte{}
	restore := func() error {
		var first error
		for p, data := range origs {
			if err := os.WriteFile(p, data, 0o644); err != nil && first == nil {
				first = err
			}
		}
		return first
	}
	files := make([]string, 0, len(r.marked))
	for f := range r.marked {
		files = append(files, f)
	}
	slices.Sort(files)
	for _, f := range files {
		p := filepath.Join(r.dir, filepath.FromSlash(f))
		data, err := os.ReadFile(p)
		if err != nil {
			restore()
			return nil, err
		}
		origs[p] = data
		marked := append([]byte{}, data...)
		if len(marked) > 0 && marked[len(marked)-1] != '\n' {
			marked = append(marked, '\n')
		}
		marked = fmt.Appendf(marked, "%s hadome mutate baseline %s-%d\n", r.marked[f], r.mark, r.runs)
		if err := os.WriteFile(p, marked, 0o644); err != nil {
			restore()
			return nil, err
		}
	}
	return restore, nil
}

// lineComment is how a line comment starts in a language that has mutants.
func lineComment(language string) string {
	if language == "python" {
		return "#"
	}
	return "//"
}

// run starts a command in the copy and reports whether it passed, and whether it went past limit.
// A run that times out did not pass.
func (r *Runner) run(argv []string, limit time.Duration) (string, bool, bool, error) {
	ctx, cancel, seen := r.clock.deadline(argv, limit)
	defer cancel()
	var out bytes.Buffer
	var w io.Writer = &out
	if seen != nil {
		w = io.MultiWriter(&out, seen)
	}
	passed, err := runTree(ctx, r.dir, argv, w)
	return out.String(), passed, ctx.Err() != nil, err
}

// runTree runs a command in dir until it ends or ctx is done, writing what it prints to out, and
// reports whether it passed. The command is the root of a tree of processes, such as go test and
// the test binaries it starts, and nothing of the tree outlives the run: ctx being done ends the
// whole tree, and whatever the command leaves running when it exits is ended without a word.
func runTree(ctx context.Context, dir string, argv []string, out io.Writer) (bool, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = dir
	cmd.Stdout, cmd.Stderr = out, out
	cmd.WaitDelay = 5 * time.Second // a child that keeps the output open must not hang the run
	tree, err := ownTree(cmd)
	if err != nil {
		return false, err
	}
	defer tree.end()
	if err := cmd.Start(); err != nil {
		return false, err // the command could not start: a configuration problem
	}
	if err := tree.started(); err != nil {
		cmd.Process.Kill()
		cmd.Wait()
		return false, err
	}
	err = cmd.Wait()
	var exit *exec.ExitError
	switch {
	case ctx.Err() != nil:
		return false, nil
	case errors.As(err, &exit):
		return false, nil
	case errors.Is(err, exec.ErrWaitDelay):
		return true, nil // the command passed, and what kept its output open is ended with the tree
	case err != nil:
		return false, err
	}
	return true, nil
}

// stages are the commands that judge a mutant, narrowest first and without repeats: the tests that
// call the broken function in their packages, the same tests in every package, then every test.
// Each runs a part of the tests the next one runs.
func stages(test []string, sep string, tests, pkgs []string) [][]string {
	var out [][]string
	add := func(cmd []string) {
		for _, c := range out {
			if slices.Equal(c, cmd) {
				return
			}
		}
		out = append(out, cmd)
	}
	if len(tests) > 0 {
		if len(pkgs) > 0 {
			add(fill(test, sep, tests, pkgs))
		}
		add(fill(test, sep, tests, nil))
	}
	add(fill(test, sep, nil, nil))
	return out
}

// fill puts the tests and the packages into the command. No tests takes {tests} out: the element
// that holds it, and the flag before it. No packages puts every package in place of {packages}.
func fill(test []string, sep string, tests, pkgs []string) []string {
	var out []string
	for i, a := range test {
		switch {
		case a == packagesHole && len(pkgs) == 0:
			out = append(out, allPackages)
		case a == packagesHole:
			out = append(out, pkgs...)
		case strings.Contains(a, testsHole) && len(tests) == 0:
			if i > 0 && strings.HasPrefix(test[i-1], "-") && len(out) > 0 {
				out = out[:len(out)-1]
			}
		case strings.Contains(a, testsHole):
			out = append(out, strings.ReplaceAll(a, testsHole, strings.Join(tests, sep)))
		default:
			out = append(out, a)
		}
	}
	return out
}

// hasPackages reports whether the command narrows by package.
func hasPackages(test []string) bool { return slices.Contains(test, packagesHole) }

// goPackages are the Go packages of the tests, as go test names them from the directory it runs in:
// the repository root, or the directory of -C in the command. It returns nil when a test is not a Go
// test or is outside that directory: narrowing by package would leave it out.
func goPackages(test []string, tests []facts.Test) []string {
	root, ok := goDir(test)
	if !ok {
		return nil
	}
	var out []string
	for _, t := range tests {
		if !strings.HasSuffix(t.Path, "_test.go") {
			return nil
		}
		dir := path.Dir(t.Path)
		switch {
		case root == ".":
		case dir == root:
			dir = "."
		case strings.HasPrefix(dir, root+"/"):
			dir = strings.TrimPrefix(dir, root+"/")
		default:
			return nil
		}
		pkg := "./" + dir
		if dir == "." {
			pkg = "."
		}
		if !slices.Contains(out, pkg) {
			out = append(out, pkg)
		}
	}
	slices.Sort(out)
	return out
}

// goDir is the directory, relative to the root of the copy, that the command's -C makes go run in.
// It is not ok when that directory cannot be seen from the root.
func goDir(test []string) (string, bool) {
	dir := "."
	for i, a := range test {
		var v string
		switch {
		case a == "-C" && i+1 < len(test):
			v = test[i+1]
		case strings.HasPrefix(a, "-C="):
			v = strings.TrimPrefix(a, "-C=")
		default:
			continue
		}
		if filepath.IsAbs(v) || path.IsAbs(filepath.ToSlash(v)) {
			return "", false
		}
		if dir = path.Join(dir, filepath.ToSlash(v)); dir == ".." || strings.HasPrefix(dir, "../") {
			return "", false
		}
	}
	return dir, true
}

func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}
