package reuse

import (
	"crypto/sha256"
	"fmt"
	"reflect"
	"sync"
)

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
	e, err := st.do(real, key, a)
	if err != nil {
		var zero T
		return zero, err
	}
	if r.rec != nil {
		if e.unreusable {
			r.rec.unanswered()
		}
		r.rec.add(read{op: "stage", arg: string(key[:]), answer: e.print, refresh: func(other *Run) (string, error) {
			e, err := st.do(other, key, a)
			if err != nil {
				return "", err
			}
			return e.print, nil
		}})
	}
	return e.value.(T), nil
}

func (st *Reading[A, T]) do(r *Run, key Hash, a A) (*entry, error) {
	return r.s.do(r, key, func() (*entry, error) {
		rc := &recorder{run: r, seen: map[string]bool{}}
		rc.inner = &Run{s: r.s, label: r.label, fsys: r.fsys, cfg: r.cfg, cfgAnswer: r.cfgAnswer, outer: r, rec: rc}
		v, err := st.fn(rc, a)
		if err != nil {
			return nil, err
		}
		e := &entry{id: st.id, key: key, scope: st.scope, value: v, reads: rc.reads, reading: true, contents: rc.contents,
			unreusable: rc.unreusable}
		if st.plain {
			e.print = valuePrint(v)
		} else {
			e.print = readsPrint(key, rc.reads)
		}
		return e, nil
	}, st.id)
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
