package golang

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/facts"
)

// callFacts writes a symbol's facts for the call diagram.
func callFacts(t *testing.T, a *facts.Analysis, id string) string {
	t.Helper()
	for _, s := range a.Symbols {
		if s.ID == id {
			return fmt.Sprintf("calls=%v through=%v supertypes=%v", s.Calls, s.Through, s.Supertypes)
		}
	}
	t.Fatalf("no symbol %s", id)
	return ""
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A Go type implements one of the repository's interfaces when its method set, with pointer
// receivers and the methods of embedded repository types, holds every method by name and by the
// types as written, resolved to their packages. A method of another module's embedded type is
// unknown, so it never completes a match; an interface without methods is never listed; an interface
// lists the interfaces it embeds.
func TestGoSupertypesMatchMethodSets(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"port/port.go": {Data: []byte("package port\n\nimport \"context\"\n\ntype User struct{}\n\n" +
			"type Finder interface{ Find(ctx context.Context, id string) (*User, error) }\n" +
			"type Saver interface{ Save(u User) error }\n" +
			"type Store interface {\n\tFinder\n\tSaver\n}\n" +
			"type Any interface{}\n" +
			"type Closer interface{ Close() error }\n")},
		"pg/pg.go": {Data: []byte("package pg\n\nimport (\n\tctx \"context\"\n\t\"io\"\n\n\t\"example.com/app/port\"\n)\n\n" +
			"type base struct{}\n\nfunc (base) Save(u port.User) error { return nil }\n\n" +
			"type Repo struct{ *base }\n\nfunc (r *Repo) Find(c ctx.Context, id string) (*port.User, error) { return nil, nil }\n\n" +
			"type Wrong struct{}\n\nfunc (Wrong) Find(c ctx.Context, id int) (*port.User, error) { return nil, nil }\n\n" +
			"type Wrapped struct{ io.ReadCloser }\n")},
	})
	cases := map[string]string{
		"example.com/app/pg.Repo":     "calls=[] through=[] supertypes=[example.com/app/port.Finder example.com/app/port.Saver example.com/app/port.Store]",
		"example.com/app/pg.Wrong":    "calls=[] through=[] supertypes=[]",
		"example.com/app/pg.Wrapped":  "calls=[] through=[] supertypes=[]",
		"example.com/app/port.Store":  "calls=[] through=[] supertypes=[example.com/app/port.Finder example.com/app/port.Saver]",
		"example.com/app/port.Finder": "calls=[] through=[] supertypes=[]",
	}
	for id, want := range cases {
		if got := callFacts(t, a, id); got != want {
			t.Errorf("%s:\n got  %s\n want %s", id, got, want)
		}
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A method called on a parameter declared with one of the repository's interfaces is a call
// through it; one on a field of an interface type is already among the calls, and one on another
// module's interface is neither.
func TestGoCallsThroughAnInterfaceParameter(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod":       {Data: []byte("module example.com/app\n")},
		"port/port.go": {Data: []byte("package port\n\ntype Finder interface{ Find(id string) string }\n")},
		"api/api.go": {Data: []byte("package api\n\nimport (\n\t\"io\"\n\n\t\"example.com/app/port\"\n)\n\n" +
			"type H struct{ f port.Finder }\n\nfunc (h H) Show(id string) string { return h.f.Find(id) }\n\n" +
			"func Use(f port.Finder, w io.Writer) string {\n\tw.Write(nil)\n\treturn f.Find(\"x\")\n}\n")},
	})
	if got := callFacts(t, a, "example.com/app/api.Use"); got != "calls=[] through=[example.com/app/port.Finder.Find] supertypes=[]" {
		t.Errorf("Use: %s", got)
	}
	if got := callFacts(t, a, "example.com/app/api.H.Show"); got != "calls=[example.com/app/port.Finder.Find] through=[] supertypes=[]" {
		t.Errorf("Show: %s", got)
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Signatures are compared type by type as written: a method differing from the interface's only in
// a slice's, an array's, a map's, a channel's, a function's, a generic type's or a literal type's
// element is no match. An interface embedding another module's interface has methods hadome does
// not know, so no type implements it; embedding that loops ends.
func TestGoSignaturesCompareEveryKindOfType(t *testing.T) {
	sig := "a []string, b [2]int, c map[string]int, d chan<- int, e func(int) error, f Box[int], g Pair[int, string], h interface{}"
	types := map[string]string{
		"Right":  sig,
		"Slice":  strings.Replace(sig, "a []string", "a []int", 1),
		"Array":  strings.Replace(sig, "b [2]int", "b [3]int", 1),
		"Sliced": strings.Replace(sig, "b [2]int", "b []int", 1),
		"Map":    strings.Replace(sig, "c map[string]int", "c map[string]bool", 1),
		"Chan":   strings.Replace(sig, "d chan<- int", "d chan<- bool", 1),
		"Func":   strings.Replace(sig, "e func(int) error", "e func(bool) error", 1),
		"Index":  strings.Replace(sig, "f Box[int]", "f Box[bool]", 1),
		"List":   strings.Replace(sig, "g Pair[int, string]", "g Pair[int, int]", 1),
		"Lit":    strings.Replace(sig, "h interface{}", "h struct{}", 1),
	}
	src := "package p\n\nimport \"io\"\n\ntype Box[T any] struct{}\ntype Pair[K, V any] struct{}\n\n" +
		"type Port interface{ Do(" + sig + ") }\n" +
		"type Closing interface {\n\tio.Closer\n\tName() string\n}\n" +
		"type Named struct{}\n\nfunc (Named) Name() string { return \"\" }\n\n" +
		"type A struct{ B }\ntype B struct{ A }\n\nfunc (A) Name() string { return \"\" }\n"
	for name, s := range types {
		src += "\ntype " + name + " struct{}\n\nfunc (" + name + ") Do(" + s + ") {}\n"
	}
	a := analyzeTree(t, fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}, "p/p.go": {Data: []byte(src)}})
	for name := range types {
		want := "calls=[] through=[] supertypes=[]"
		if name == "Right" {
			want = "calls=[] through=[] supertypes=[example.com/app/p.Port]"
		}
		if got := callFacts(t, a, "example.com/app/p."+name); got != want {
			t.Errorf("%s: got %s, want %s", name, got, want)
		}
	}
	for _, id := range []string{"example.com/app/p.Named", "example.com/app/p.B"} {
		if got := callFacts(t, a, id); got != "calls=[] through=[] supertypes=[]" {
			t.Errorf("%s implements nothing hadome knows in full, got %s", id, got)
		}
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// The calls through interfaces are listed sorted, whatever order the body makes them in.
func TestGoCallsThroughAreSorted(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod":       {Data: []byte("module example.com/app\n")},
		"port/port.go": {Data: []byte("package port\n\ntype I interface {\n\tD()\n\tC()\n\tB()\n\tA()\n}\n")},
		"api/api.go":   {Data: []byte("package api\n\nimport \"example.com/app/port\"\n\nfunc Use(i port.I) {\n\ti.D()\n\ti.B()\n\ti.C()\n\ti.A()\n}\n")},
	})
	want := "calls=[] through=[example.com/app/port.I.A example.com/app/port.I.B example.com/app/port.I.C example.com/app/port.I.D] supertypes=[]"
	if got := callFacts(t, a, "example.com/app/api.Use"); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
}
