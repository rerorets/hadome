package reuse

import (
	"io/fs"
	"sync"

	"github.com/rerorets/hadome/internal/config"
)

// Run is one version as the session reads it.
type Run struct {
	s         *Session
	label     string
	fsys      fs.FS
	cfg       config.Analysis
	cfgAnswer string

	mu      sync.Mutex
	answers map[string]string // answers already computed on this version, by read
	// outer and rec are set for the run a reading stage reads through: outer is the real run, and
	// rec records every read.
	outer *Run
	rec   *recorder
}

// Run returns the session's run of a version, read from fsys with configuration c.
func (s *Session) Run(label string, fsys fs.FS, c config.Analysis) *Run {
	h, _ := encode(c)
	return &Run{s: s, label: label, fsys: fsys, cfg: c, cfgAnswer: contentAnswer(h), answers: map[string]string{}}
}

// Lone returns a run of a version in a session of its own, with reuse and the built-in
// configuration, for reading one version outside an analysis call. Its results are never let go,
// so it is meant for tests.
func Lone(fsys fs.FS) *Run { return NewSession(nil, true).Run("", fsys, config.Analysis{}) }

// Label is the version's label.
func (r *Run) Label() string { return r.label }

// FS is the version. Inside a reading stage, every read of it is recorded.
func (r *Run) FS() fs.FS {
	if r.rec != nil {
		return recordingFS{r.rec}
	}
	return r.fsys
}

// Config is the configuration the version is analysed with.
func (r *Run) Config() config.Analysis {
	if r.rec != nil {
		r.rec.add(read{op: "config", answer: r.cfgAnswer})
	}
	return r.cfg
}

func (r *Run) real() *Run {
	if r.outer != nil {
		return r.outer
	}
	return r
}

func (r *Run) recalled(op, name string) (string, bool) {
	r = r.real()
	r.mu.Lock()
	defer r.mu.Unlock()
	a, ok := r.answers[op+"\x00"+name]
	return a, ok
}

func (r *Run) remember(op, name, answer string) {
	r = r.real()
	r.mu.Lock()
	r.answers[op+"\x00"+name] = answer
	r.mu.Unlock()
}

// Reads is how a reading stage sees the version. Every read through it is recorded.
type Reads interface {
	// FS is the version. Opening, reading, listing and stating its files are recorded reads.
	FS() fs.FS
	// Config is the configuration, a recorded read.
	Config() config.Analysis
	// Env is what both versions share.
	Env() any
	// Run is the run to hand to code that calls stages or reads the version: its FS records, and a
	// reading stage called with it is recorded as a read.
	Run() *Run
}

type read struct {
	op, arg, answer string
	// refresh gives the answer of a read of another stage on a run: that stage's fingerprint there.
	refresh func(r *Run) (string, error)
}

type recorder struct {
	run      *Run // the real run
	inner    *Run
	mu       sync.Mutex
	reads    []read
	contents []Hash
	// unreusable is set by a read that gave no answer: the result may not be reused.
	unreusable bool
}

// read keeps the hash of a file content the result is computed from.
func (rc *recorder) read(sum Hash) {
	rc.mu.Lock()
	rc.contents = append(rc.contents, sum)
	rc.mu.Unlock()
}

func (rc *recorder) unanswered() {
	rc.mu.Lock()
	rc.unreusable = true
	rc.mu.Unlock()
}

func (rc *recorder) add(rd read) {
	rc.mu.Lock()
	rc.reads = append(rc.reads, rd)
	rc.mu.Unlock()
}

func (rc *recorder) FS() fs.FS               { return recordingFS{rc} }
func (rc *recorder) Config() config.Analysis { return rc.inner.Config() }
func (rc *recorder) Env() any                { return rc.run.s.env }
func (rc *recorder) Run() *Run               { return rc.inner }
