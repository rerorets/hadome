// Package reuse remembers the results of adapter stages within one analysis call, so that a stage
// called on head returns what it computed on base whenever its input is the same in both.
//
// A pure stage sees only its argument, and is reused when its key (its name and its whole
// argument) is the same. A reading stage also reads the version, through Reads, and is reused when
// every read it recorded gives the same answer on the other version.
//
// Design: docs/design/analysis-reuse.md
package reuse

import (
	"crypto/sha256"
	"fmt"
	"io/fs"
	"reflect"
	"sort"
	"sync"

	"github.com/rerorets/hadome/internal/adapter/walk"
	"github.com/rerorets/hadome/internal/config"
)

// Scope is how long a stage's results are remembered.
type Scope int

const (
	// ScopeLanguage results are let go when head's analysis of the language ends.
	ScopeLanguage Scope = iota
	// ScopePair results are let go when the analysis call ends.
	ScopePair
)

// Releaser is a result holding memory outside Go, such as a tree-sitter syntax tree. The session
// calls Release once, when it lets go of the result.
type Releaser interface{ Release() }

// Stat counts how often a stage's remembered result was returned, and how often it ran.
type Stat struct{ Hits, Misses int }

// Session remembers stage results for one analysis call. It is safe for concurrent use.
type Session struct {
	env   any
	reuse bool

	mu       sync.Mutex
	entries  map[Hash]*entry
	created  []*entry // since the last Settle or EndLanguage
	live     []*entry // not let go yet
	head     map[Hash]bool
	inflight map[Hash]chan struct{}
	stats    map[string]*Stat
}

// NewSession returns a session. env is what both versions share, such as the languages, which
// reading stages may use through Reads.Env. Without reuse, the session never returns a remembered
// result: stages run exactly as with reuse, only every call runs its stage.
func NewSession(env any, reuse bool) *Session {
	return &Session{env: env, reuse: reuse, entries: map[Hash]*entry{}, inflight: map[Hash]chan struct{}{},
		stats: map[string]*Stat{}}
}

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

// Env is what the session was given to share between versions.
func (r *Run) Env() any { return r.s.env }

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

// Expect tells the session which version head is: it hashes the content of every file of head, so
// that a result computed on base is remembered only when head can use it.
func (s *Session) Expect(head *Run) error {
	paths, err := walk.Files(head.fsys)
	if err != nil {
		return err
	}
	contents := map[Hash]bool{}
	for _, p := range paths {
		_, answer, err := head.readFile(p)
		if err != nil {
			return err
		}
		if h, ok := contentOf(answer); ok {
			contents[h] = true
		}
	}
	s.mu.Lock()
	s.head = contents
	s.mu.Unlock()
	return nil
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
	seen     map[string]bool
	contents []Hash
}

func (rc *recorder) add(rd read) {
	rc.mu.Lock()
	defer rc.mu.Unlock()
	k := rd.op + "\x00" + rd.arg
	if rc.seen[k] {
		return
	}
	rc.seen[k] = true
	rc.reads = append(rc.reads, rd)
	if h, ok := contentOf(rd.answer); ok && rd.op == "file" {
		rc.contents = append(rc.contents, h)
	}
}

func (rc *recorder) FS() fs.FS               { return recordingFS{rc} }
func (rc *recorder) Config() config.Analysis { return rc.inner.Config() }
func (rc *recorder) Env() any                { return rc.run.s.env }
func (rc *recorder) Run() *Run               { return rc.inner }

type entry struct {
	id       string
	key      Hash
	scope    Scope
	run      *Run // the real run that computed it
	value    any
	reads    []read // nil for a pure stage
	reading  bool
	contents []Hash // the file contents it was computed from
	print    string // its fingerprint
	valid    map[*Run]bool
	gone     bool
}

var (
	registryMu sync.Mutex
	registry   = map[string]bool{}
)

// register claims a stage's name. A name registered twice, or an argument not made of values
// only, is a mistake in hadome and stops it as it starts.
func register(id string, arg reflect.Type) {
	if !plain(arg) {
		panic(fmt.Sprintf("reuse: the argument of stage %s, %s, is not made of values only", id, arg))
	}
	registryMu.Lock()
	defer registryMu.Unlock()
	if registry[id] {
		panic("reuse: stage " + id + " is registered twice")
	}
	registry[id] = true
}

// Pure is a stage that sees only its argument.
type Pure[A, T any] struct {
	id    string
	scope Scope
	fn    func(A) (T, error)
	plain bool
}

// NewPure registers a pure stage. fn must be a top-level function: it sees nothing but its
// argument, so that the argument is all its result depends on.
func NewPure[A, T any](id string, scope Scope, fn func(A) (T, error)) *Pure[A, T] {
	register(id, reflect.TypeFor[A]())
	return &Pure[A, T]{id: id, scope: scope, fn: fn, plain: plain(reflect.TypeFor[T]())}
}

// Do returns the stage's result for a: remembered when a stage with the same key ran in the
// session, computed otherwise.
func (st *Pure[A, T]) Do(r *Run, a A) (T, error) {
	key, contents := stageKey(st.id, a)
	e, err := r.s.do(r, key, func() (*entry, error) {
		v, err := st.fn(a)
		if err != nil {
			return nil, err
		}
		e := &entry{id: st.id, key: key, scope: st.scope, value: v, contents: contents, print: hashAnswer(key)}
		if st.plain {
			e.print = valuePrint(v)
		}
		return e, nil
	}, st.id)
	if err != nil {
		var zero T
		return zero, err
	}
	return e.value.(T), nil
}

// Reading is a stage that also reads the version, through Reads.
type Reading[A, T any] struct {
	id    string
	scope Scope
	fn    func(Reads, A) (T, error)
	plain bool
}

// NewReading registers a reading stage. fn must be a top-level function that reads the version
// only through Reads.
func NewReading[A, T any](id string, scope Scope, fn func(Reads, A) (T, error)) *Reading[A, T] {
	register(id, reflect.TypeFor[A]())
	return &Reading[A, T]{id: id, scope: scope, fn: fn, plain: plain(reflect.TypeFor[T]())}
}

// Do returns the stage's result for a: remembered when the stage ran with the same argument and
// every read it recorded gives the same answer on r's version, computed otherwise. Called with the
// run of another reading stage, the call is recorded as a read of that stage.
func (st *Reading[A, T]) Do(r *Run, a A) (T, error) {
	key, _ := stageKey(st.id, a)
	real := r.real()
	v, print, err := st.do(real, key, a)
	if err != nil {
		var zero T
		return zero, err
	}
	if r.rec != nil {
		r.rec.add(read{op: "stage", arg: string(key[:]), answer: print, refresh: func(other *Run) (string, error) {
			_, p, err := st.do(other, key, a)
			return p, err
		}})
	}
	return v, nil
}

func (st *Reading[A, T]) do(r *Run, key Hash, a A) (T, string, error) {
	e, err := r.s.do(r, key, func() (*entry, error) {
		rc := &recorder{run: r, seen: map[string]bool{}}
		rc.inner = &Run{s: r.s, label: r.label, fsys: r.fsys, cfg: r.cfg, cfgAnswer: r.cfgAnswer, outer: r, rec: rc}
		v, err := st.fn(rc, a)
		if err != nil {
			return nil, err
		}
		e := &entry{id: st.id, key: key, scope: st.scope, value: v, reads: rc.reads, reading: true, contents: rc.contents}
		if st.plain {
			e.print = valuePrint(v)
		} else {
			e.print = readsPrint(key, rc.reads)
		}
		return e, nil
	}, st.id)
	if err != nil {
		var zero T
		return zero, "", err
	}
	return e.value.(T), e.print, nil
}

func stageKey(id string, a any) (Hash, []Hash) {
	e := newEncoder()
	e.str(id)
	e.value(reflect.ValueOf(a))
	return e.sum(), e.contents
}

func hashAnswer(h Hash) string { return contentAnswer(h) }

func valuePrint(v any) string {
	h, _ := encode(v)
	return contentAnswer(h)
}

func readsPrint(key Hash, reads []read) string {
	h := sha256.New()
	h.Write(key[:])
	for _, rd := range reads {
		fmt.Fprintf(h, "%q %q %q\n", rd.op, rd.arg, rd.answer)
	}
	var out Hash
	copy(out[:], h.Sum(nil))
	return contentAnswer(out)
}

// do returns the remembered value under key when it may be used on r, and otherwise runs compute
// and remembers what it returns. Only one call computes a key at a time.
func (s *Session) do(r *Run, key Hash, compute func() (*entry, error), id string) (*entry, error) {
	for {
		s.mu.Lock()
		if wait, ok := s.inflight[key]; ok {
			s.mu.Unlock()
			<-wait
			continue
		}
		e := s.entries[key]
		if e != nil && s.reuse {
			if !e.reading || e.run == r || e.valid[r] {
				s.stat(id).Hits++
				s.mu.Unlock()
				return e, nil
			}
		}
		done := make(chan struct{})
		s.inflight[key] = done
		s.mu.Unlock()

		if e != nil && s.reuse && s.validate(e, r) {
			s.mu.Lock()
			if e.valid == nil {
				e.valid = map[*Run]bool{}
			}
			e.valid[r] = true
			s.stat(id).Hits++
			delete(s.inflight, key)
			close(done)
			s.mu.Unlock()
			return e, nil
		}
		ne, err := compute()
		s.mu.Lock()
		delete(s.inflight, key)
		close(done)
		if err != nil {
			s.mu.Unlock()
			return nil, err
		}
		ne.run = r
		s.stat(id).Misses++
		s.entries[key] = ne
		s.created = append(s.created, ne)
		s.live = append(s.live, ne)
		s.mu.Unlock()
		return ne, nil
	}
}

func (s *Session) stat(id string) *Stat {
	st := s.stats[id]
	if st == nil {
		st = &Stat{}
		s.stats[id] = st
	}
	return st
}

// validate reports whether every read e recorded gives the same answer on r.
func (s *Session) validate(e *entry, r *Run) bool {
	for _, rd := range e.reads {
		var got string
		if rd.refresh != nil {
			p, err := rd.refresh(r)
			if err != nil {
				return false
			}
			got = p
		} else {
			got = r.answer(rd.op, rd.arg)
		}
		if got != rd.answer {
			return false
		}
	}
	return true
}

// Settle lets go of the ScopeLanguage results computed since the last call that head can never
// use: one computed from a file content head does not have. Call it when base's analysis of a
// language ends.
func (s *Session) Settle() {
	s.mu.Lock()
	var drop []*entry
	if s.head != nil {
		for _, e := range s.created {
			if e.scope == ScopeLanguage && !s.hittable(e) {
				drop = append(drop, e)
			}
		}
	}
	s.created = nil
	s.forget(drop)
	s.mu.Unlock()
	release(drop)
}

func (s *Session) hittable(e *entry) bool {
	for _, c := range e.contents {
		if !s.head[c] {
			return false
		}
	}
	return true
}

// EndLanguage lets go of every ScopeLanguage result. Call it when head's analysis of a language ends.
func (s *Session) EndLanguage() { s.end(func(e *entry) bool { return e.scope == ScopeLanguage }) }

// End lets go of every result. Call it when the analysis call ends.
func (s *Session) End() { s.end(func(*entry) bool { return true }) }

func (s *Session) end(match func(*entry) bool) {
	s.mu.Lock()
	var drop []*entry
	for _, e := range s.live {
		if match(e) {
			drop = append(drop, e)
		}
	}
	s.created = nil
	s.forget(drop)
	s.mu.Unlock()
	release(drop)
}

// forget removes the entries from the session; s.mu is held.
func (s *Session) forget(drop []*entry) {
	if len(drop) == 0 {
		return
	}
	gone := map[*entry]bool{}
	for _, e := range drop {
		gone[e] = true
		e.gone = true
		if s.entries[e.key] == e {
			delete(s.entries, e.key)
		}
	}
	live := s.live[:0]
	for _, e := range s.live {
		if !gone[e] {
			live = append(live, e)
		}
	}
	s.live = live
}

func release(drop []*entry) {
	for _, e := range drop {
		if rl, ok := e.value.(Releaser); ok {
			rl.Release()
		}
	}
}

// Stats returns, by stage name, how often each stage's remembered result was returned and how
// often it ran.
func (s *Session) Stats() map[string]Stat {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]Stat{}
	for id, st := range s.stats {
		out[id] = *st
	}
	return out
}

// Remembered returns how many results the session holds, by stage name, sorted.
func (s *Session) Remembered() []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []string
	for _, e := range s.live {
		out = append(out, e.id)
	}
	sort.Strings(out)
	return out
}
