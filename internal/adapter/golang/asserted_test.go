// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package golang

import (
	"slices"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A method of an interface called on a value taken out by asking whether it holds the interface
// (errors.As, a type assertion, a case of a type switch) is among the
// symbol's Asserted calls; one called on such a value of a concrete type, on a field or on a
// parameter is not, even when the same method is also called on a value asked for. The code of a
// file outside functions has its own.
func TestGoCallsOnValuesAskedForAnInterfaceAreAsserted(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"app/app.go": {Data: []byte(`package app

import "errors"

type Finder interface{ Find() string }
type Namer interface{ Name() string }
type Sizer interface{ Size() int }
type Closer interface{ Close() }
type Opener interface{ Open() }

type Repo struct{}

func (*Repo) Find() string  { return "" }
func (*Repo) Error() string { return "" }

type Holder struct{ Opener Opener }

var Default any

var label = Default.(Namer).Name()

func Asked(err error, v any, c Closer, h Holder) {
	var f Finder
	if errors.As(err, &f) {
		f.Find()
	}
	v.(Namer).Name()
	switch x := v.(type) {
	case Sizer:
		x.Size()
	case *Repo:
		x.Find()
	}
	var r *Repo
	if errors.As(err, &r) {
		r.Error()
	}
	c.Close()
	v.(Closer).Close()
	h.Opener.Open()
	v.(Opener).Open()
}
`)},
	})
	want := []string{"example.com/app/app.Finder.Find", "example.com/app/app.Namer.Name", "example.com/app/app.Repo.Error", "example.com/app/app.Repo.Find", "example.com/app/app.Sizer.Size"}
	i := slices.IndexFunc(a.Symbols, func(s facts.Symbol) bool { return s.ID == "example.com/app/app.Asked" })
	if i < 0 {
		t.Fatal("no symbol Asked")
	}
	if s := a.Symbols[i]; !slices.Equal(s.Asserted, want) {
		t.Errorf("Asked: Asserted = %v, want %v (Calls %v, Through %v)", s.Asserted, want, s.Calls, s.Through)
	}
	for _, f := range a.Files {
		if f.Path == "app/app.go" && !slices.Equal(f.Asserted, []string{"example.com/app/app.Namer.Name"}) {
			t.Errorf("file: Asserted = %v, want [example.com/app/app.Namer.Name] (Calls %v)", f.Asserted, f.Calls)
		}
	}
}
