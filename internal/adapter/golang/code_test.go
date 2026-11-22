// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package golang

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

func goSymbol(t *testing.T, a *facts.Analysis, id string) facts.Symbol {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no symbol %s", id)
	return facts.Symbol{}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A handler registered inside a product function, whose body joins strings, is read like any other
// code: the walk of the handler has everywhere to note a string it finds.
func TestGoHandlerThatJoinsStringsIsRead(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"api/routes.go": {Data: []byte(`package api

import "net/http"

func Routes(mux *http.ServeMux, name string) {
	mux.HandleFunc("/hello", func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello " + name))
	})
}
`)},
	})
	goSymbol(t, a, "example.com/app/api.Routes")
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function's values that belong in configuration are found: a URL with a host, a host and port, a
// duration built from a number; a zero duration, a loopback address alone, a path and a constant are
// not. An error assigned to _ is dropped when the callee's last result is error, in the repository
// or in the standard library, and not when the callee is unknown or returns something else.
func TestGoConfigurationValuesAndDroppedErrors(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"client/client.go": {Data: []byte(`package client

import (
	"os"
	"strconv"
	"time"
)

type Store struct{}

func (Store) Save() error  { return nil }
func (Store) Name() string { return "" }

type saver interface{ Save() error }

func Run(s Store, x saver, n string) {
	const fixed = "https://fixed.shop.internal"
	_ = get("https://api.shop.internal/v1")
	_ = get("db.internal:5432")
	_ = get("/v1/orders")
	_ = get("127.0.0.1")
	wait(30 * time.Second)
	wait(0 * time.Second)
	_ = s.Save()
	_ = s.Name()
	_ = x.Save()
	_ = os.Remove(n)
	v, _ := strconv.Atoi(n)
	_ = v
}

func get(u string) string     { return u }
func wait(d time.Duration)    {}
`)},
	})
	s := goSymbol(t, a, "example.com/app/client.Run")
	var lits []string
	for _, l := range s.Literals {
		lits = append(lits, fmt.Sprintf("%s@%d", l.Kind, l.Line))
	}
	if got, want := strings.Join(lits, " "), "url@18 host-port@19 duration@22"; got != want {
		t.Errorf("got literals %q, want %q", got, want)
	}
	var drops []string
	for _, w := range s.Swallows {
		drops = append(drops, fmt.Sprint(w.Line))
	}
	if got, want := strings.Join(drops, " "), "24 27 28"; got != want {
		t.Errorf("got dropped errors at %q, want %q: %v", got, want, s.Swallows)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Two functions that differ only in their names, parameters and local variables have the same Body;
// a different literal, a different field read, or the same variables in another order makes
// another one.
func TestGoBodiesIgnoreLocalNames(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"calc/calc.go": {Data: []byte(`package calc

type T struct{ total, sum float64 }

func A(t T, total float64) float64 { scale := 10.0; return total*scale/scale + t.total }
func B(u T, sum float64) float64   { f := 10.0; return sum*f/f + u.total }
func C(u T, sum float64) float64   { f := 100.0; return sum*f/f + u.total }
func D(u T, sum float64) float64   { f := 10.0; return sum*f/f + u.sum }
func E(u T, sum float64) float64   { f := 10.0; return f*sum/f + u.total }
`)},
	})
	sym := func(name string) facts.Symbol { return goSymbol(t, a, "example.com/app/calc."+name) }
	if sym("A").Body == "" || sym("A").Body != sym("B").Body {
		t.Errorf("A and B differ only in local names but have different bodies")
	}
	if sym("A").Body == sym("C").Body {
		t.Errorf("C has another literal but the same body as A")
	}
	if sym("A").Body == sym("D").Body {
		t.Errorf("D reads another field but has the same body as A")
	}
	if sym("A").Body == sym("E").Body {
		t.Errorf("E uses its variables in another order but has the same body as A")
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A URL compared with a value or given to strings.HasPrefix, and a duration added to a time, given to
// Add or compared with one, are recognised or calculated with, not settings; a URL joined with + and
// a duration given to anything else still count.
func TestGoValuesComparedOrCalculatedWith(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"client/client.go": {Data: []byte(`package client

import (
	"strings"
	"time"
)

func Run(u string, now time.Time) (string, time.Time) {
	if strings.HasPrefix(u, "https://git.shop.internal/") || "https://git.shop.internal/" == u {
		return "", now.Add(-24 * time.Hour)
	}
	if now.Sub(now) > 5*time.Minute {
		return "", now.Add(2*time.Hour + 30*time.Minute)
	}
	wait(30*time.Second + 5*time.Second)
	return "https://api.shop.internal" + u, now
}

func wait(d time.Duration) {}
`)},
	})
	var lits []string
	for _, l := range goSymbol(t, a, "example.com/app/client.Run").Literals {
		lits = append(lits, fmt.Sprintf("%s@%d", l.Kind, l.Line))
	}
	if got, want := strings.Join(lits, " "), "duration@15 duration@15 url@16"; got != want {
		t.Errorf("got literals %q, want %q", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// At the edges of the shapes: a literal assigned to a field named for a time limit or a port counts,
// a value assigned to it does not, nor a literal assigned along with another; a negative or a
// fractional number of time units is a duration, zero in any spelling is not; a string given to a
// port is no port, and a string given to a name the configuration lists counts. A duration added to
// another value, or given to a function of the package that compares text, is calculated or compared
// with.
func TestGoValuesAtTheEdgesOfTheirShapes(t *testing.T) {
	tree := fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"client/client.go": {Data: []byte(`package client

import "time"

type Opts struct {
	Timeout time.Duration
	Port    string
	Bucket  string
}

func Configure(o *Opts, n int, elapsed time.Duration, list []string) {
	o.Timeout = 30
	o.Timeout = elapsed
	o.Port, o.Bucket = "a", "b"
	wait(-30 * time.Second)
	wait(1.5 * time.Second)
	wait(0.0 * time.Second)
	_ = Opts{Port: "auto"}
	_ = Opts{Bucket: "uploads"}
	wait(elapsed + 5*time.Second)
	_ = contains(list, "https://git.shop.internal/")
	wait(elapsed - 5*time.Second)
}

func wait(d time.Duration) {}

func contains(list []string, s string) bool { return false }
`)},
	}
	a, err := Adapter{}.Analyze(reuse.Lone(tree), config.Analysis{ConfigNames: []string{"bucket"}}, []string{"client/client.go"})
	if err != nil {
		t.Fatal(err)
	}
	var lits []string
	for _, l := range goSymbol(t, a, "example.com/app/client.Configure").Literals {
		lits = append(lits, fmt.Sprintf("%s@%d", l.Kind, l.Line))
	}
	if got, want := strings.Join(lits, " "), "duration@12 duration@15 duration@16 named@19"; got != want {
		t.Errorf("got literals %q, want %q", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// An error assigned to _ is dropped when the callee is a function of the repository, or a method of
// a standard library type such as *os.File, declared to return one last. A function held in a local
// variable that shadows such a function, a builtin, and a function held in a field, of a literal
// struct type or of a type of the repository, are not known to return an error.
func TestGoDroppedErrorsByCallee(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"files/files.go": {Data: []byte(`package files

import "os"

func cleanup() error { return nil }

func get() error { return nil }

type Job struct{ run func() error }

func Run(n string, f *os.File, h struct{ run func() error }, j Job) {
	_ = cleanup()
	g, _ := os.Open(n)
	_ = f.Close()
	_ = g
	get := func() int { return 1 }
	_ = get()
	_ = len(n)
	_ = h.run()
	_ = j.run()
}
`)},
	})
	var drops []string
	for _, w := range goSymbol(t, a, "example.com/app/files.Run").Swallows {
		drops = append(drops, fmt.Sprint(w.Line))
	}
	if got, want := strings.Join(drops, " "), "12 13 14"; got != want { // cleanup, os.Open, (*os.File).Close
		t.Errorf("got dropped errors at %q, want %q", got, want)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Where the lines break does not change a body: a function written on one line has the body of the
// same function written over several, braces and parentheses alike.
func TestGoBodiesDoNotDependOnLineBreaks(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"calc/calc.go": {Data: []byte(`package calc

func A(total float64) float64 { var ( scale = 10.0 ); return total*scale/scale + 1 }

func B(sum float64) float64 {
	var (
		f = 10.0
	)
	return sum*f/f + 1
}
`)},
	})
	if x, y := goSymbol(t, a, "example.com/app/calc.A").Body, goSymbol(t, a, "example.com/app/calc.B").Body; x == "" || x != y {
		t.Errorf("A and B differ only in their line breaks but have different bodies")
	}
}
