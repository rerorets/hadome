package golang

import (
	"fmt"
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
