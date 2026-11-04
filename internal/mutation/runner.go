package mutation

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Outcome is what the tests did with one mutant.
type Outcome int

// Outcomes.
const (
	Caught Outcome = iota
	Survived
)

// Runner runs the test command in a copy of one version. It never writes to the repository's
// working tree: only the copy is broken, and each mutant is restored before the next.
type Runner struct {
	Test    []string // the configured command; an element may hold {tests}
	Sep     string   // what joins test names in {tests}
	Timeout time.Duration
	dir     string
}

// copyVersion writes a version's files into a fresh temporary directory.
func copyVersion(v engine.Version) (string, error) {
	dir, err := os.MkdirTemp("", "hadome-mutate-")
	if err != nil {
		return "", err
	}
	err = fs.WalkDir(v.FS, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		data, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return err
		}
		dst := filepath.Join(dir, filepath.FromSlash(p))
		if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
			return err
		}
		return os.WriteFile(dst, data, 0o644)
	})
	if err != nil {
		os.RemoveAll(dir)
		return "", err
	}
	return dir, nil
}

// clean runs every test once on the unbroken copy and reports how long it took. Failing tests make
// every mutant look caught, so they stop the run.
func (r *Runner) clean() (time.Duration, error) {
	start := time.Now()
	limit := r.Timeout
	if limit == 0 {
		limit = time.Hour // nothing is known yet; a hung clean run still ends
	}
	out, ok, err := r.run(r.full(), limit)
	if err != nil {
		return 0, err
	}
	if !ok {
		return 0, errors.New("the tests fail with nothing broken, so no mutant can be judged; fix the tests first:\n" + tail(out))
	}
	return time.Since(start), nil
}

// try applies one mutant, runs the tests (only those named, when the command holds {tests}), and
// restores the file. A survivor of a narrowed run is run again with every test.
func (r *Runner) try(m facts.Mutant, tests []string) (Outcome, error) {
	p := filepath.Join(r.dir, filepath.FromSlash(m.Path))
	orig, err := os.ReadFile(p)
	if err != nil {
		return Caught, err
	}
	broken := append(append(append([]byte{}, orig[:m.Start]...), m.Replacement...), orig[m.End:]...)
	if err := os.WriteFile(p, broken, 0o644); err != nil {
		return Caught, err
	}
	defer os.WriteFile(p, orig, 0o644)
	cmd := r.full()
	narrowed := r.narrowable() && len(tests) > 0
	if narrowed {
		cmd = r.narrow(tests)
	}
	_, passed, err := r.run(cmd, r.Timeout)
	if err != nil || !passed {
		return Caught, err
	}
	if narrowed {
		if _, passed, err = r.run(r.full(), r.Timeout); err != nil || !passed {
			return Caught, err
		}
	}
	return Survived, nil
}

// run starts a command in the copy and reports whether it passed. A run that times out did not
// pass: a mutant that made the tests stop finishing was noticed.
func (r *Runner) run(argv []string, limit time.Duration) (string, bool, error) {
	ctx, cancel := context.WithTimeout(context.Background(), limit)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	cmd.Dir = r.dir
	cmd.WaitDelay = 5 * time.Second // a child that keeps the output open must not hang the run
	out, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return string(out), false, nil
	case errors.As(err, &exit):
		return string(out), false, nil
	case err != nil:
		return string(out), false, err // the command could not start: a configuration problem
	}
	return string(out), true, nil
}

func (r *Runner) narrowable() bool {
	for _, a := range r.Test {
		if strings.Contains(a, "{tests}") {
			return true
		}
	}
	return false
}

// narrow fills {tests} with the names, joined by the separator.
func (r *Runner) narrow(tests []string) []string {
	out := make([]string, len(r.Test))
	for i, a := range r.Test {
		out[i] = strings.ReplaceAll(a, "{tests}", strings.Join(tests, r.Sep))
	}
	return out
}

// full is the command with {tests} taken out: the element that holds it, and the flag before it.
func (r *Runner) full() []string {
	var out []string
	for i, a := range r.Test {
		if strings.Contains(a, "{tests}") {
			if i > 0 && strings.HasPrefix(r.Test[i-1], "-") && len(out) > 0 {
				out = out[:len(out)-1]
			}
			continue
		}
		out = append(out, a)
	}
	return out
}

func tail(s string) string {
	lines := strings.Split(strings.TrimRight(s, "\n"), "\n")
	if len(lines) > 20 {
		lines = lines[len(lines)-20:]
	}
	return strings.Join(lines, "\n")
}
