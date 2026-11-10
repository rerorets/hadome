package mutation

import (
	"context"
	"io"
	"os"
	"strings"
	"time"
)

// A run with a mutant has a time limit worked out from its command's run before breaking, and a
// timeout counts as caught only once that limit is confirmed (docs/design/mutation.md, "Confirming
// the limit").

// clock measures runs and ends a run that goes past its limit. Tests replace it, so that which runs
// time out, and how long the runs before breaking take, do not depend on the machine's load.
type clock struct {
	// start begins measuring a run and returns what tells how long it has taken so far.
	start func() func() time.Duration
	// deadline returns the context of a run of argv, done once the run has gone past limit. When it
	// returns a writer, the run's output goes there too as it comes.
	deadline func(argv []string, limit time.Duration) (context.Context, context.CancelFunc, io.Writer)
}

// wallClock is the clock of hadome mutate: real time.
var wallClock = clock{
	start: func() func() time.Duration {
		t := time.Now()
		return func() time.Duration { return time.Since(t) }
	},
	deadline: func(_ []string, limit time.Duration) (context.Context, context.CancelFunc, io.Writer) {
		ctx, cancel := context.WithTimeout(context.Background(), limit)
		return ctx, cancel, nil
	},
}

// confirmations is how many times a run with a mutant that timed out runs again, each time after its
// command's run before breaking found the machine slower (see Runner.try).
const confirmations = 2

// limitAfter is the limit of a run with a mutant, from how long the command took with nothing
// broken: three times that; the second covers a run of almost nothing.
func limitAfter(took time.Duration) time.Duration {
	return 3*took + time.Second
}

// measure runs a command with nothing broken, with a comment added to every file that holds a
// mutant, and tells how long it took.
func (r *Runner) measure(cmd []string) (string, bool, time.Duration, error) {
	restore, err := r.addMarks()
	if err != nil {
		return "", false, 0, err
	}
	limit := r.Timeout
	if limit == 0 {
		limit = time.Hour // nothing is known yet; a hung run still ends
	}
	took := r.clock.start()
	out, passed, _, err := r.run(cmd, limit)
	d := took()
	if rerr := restore(); err == nil {
		err = rerr
	}
	return out, passed, d, err
}

// confirm runs the command again with nothing broken, after a run with the mutant in p timed out,
// and reports whether the limit worked out from it is longer: the machine got slower, so the limit
// is raised for this and later mutants. A run that fails confirms nothing. p holds broken again
// when it returns.
func (r *Runner) confirm(cmd []string, p string, orig, broken []byte) (bool, error) {
	if err := os.WriteFile(p, orig, 0o644); err != nil {
		return false, err
	}
	_, passed, took, err := r.measure(cmd)
	if werr := os.WriteFile(p, broken, 0o644); err == nil {
		err = werr
	}
	if err != nil || !passed {
		return false, err
	}
	key := strings.Join(cmd, "\x00")
	b := r.before[key]
	if limit := limitAfter(took); limit > b.limit {
		b.limit = limit
		r.before[key] = b
		return true, nil
	}
	return false, nil
}
