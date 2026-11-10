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
	"sort"
	"sync"
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

// Stat counts how often a stage's remembered result was returned, how often it ran, and how often a
// call waited for another call running the stage with the same key.
type Stat struct{ Hits, Misses, Waits int }

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

// Expect tells the session which version head is, and which files it has: it hashes the content
// of each, so that a result computed on base is remembered only when head can use it.
func (s *Session) Expect(head *Run, paths []string) error {
	contents := map[Hash]bool{}
	for _, p := range paths {
		_, sum, _, _, err := head.readFile(p)
		if err != nil {
			return err
		}
		contents[sum] = true
	}
	s.mu.Lock()
	s.head = contents
	s.mu.Unlock()
	return nil
}

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
	// unreusable is a result one of whose reads gave no answer; it is never served to another run.
	unreusable bool
}

// do returns the remembered value under key when it may be used on r, and otherwise runs compute
// and remembers what it returns. Only one call computes a key at a time.
func (s *Session) do(r *Run, key Hash, compute func() (*entry, error), id string) (*entry, error) {
	for {
		s.mu.Lock()
		if wait, ok := s.inflight[key]; ok {
			s.stat(id).Waits++
			s.mu.Unlock()
			<-wait
			continue
		}
		e := s.entries[key]
		if e != nil && s.reuse {
			if !e.reading || e.run == r {
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
	if e.unreusable {
		return false
	}
	for _, rd := range e.reads {
		var got string
		if rd.refresh != nil {
			p, err := rd.refresh(r)
			if err != nil {
				return false
			}
			got = p
		} else {
			a, ok := r.answer(rd.op, rd.arg)
			if !ok {
				return false
			}
			got = a
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
