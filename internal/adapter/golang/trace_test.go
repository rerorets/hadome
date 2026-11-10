package golang

import (
	"fmt"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

func analyzeTree(t *testing.T, tree fstest.MapFS) *facts.Analysis {
	t.Helper()
	var paths []string
	for p := range tree {
		if strings.HasSuffix(p, ".go") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	a, err := Adapter{}.Analyze(reuse.Lone(tree), config.Analysis{}, paths)
	if err != nil {
		t.Fatal(err)
	}
	return a
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// Registering routes is no input or output of the function that registers them. A handler is
// found as a function of another package, as a method value whose signature is not a handler's,
// and inside http.HandlerFunc(…); a value made by any other call is no entry point. A function
// literal's parameter hides a package of the same name, so the literal calls nothing of it.
func TestGoRouteRegistrationsAndHandlers(t *testing.T) {
	a := analyzeTree(t, fstest.MapFS{
		"go.mod":           {Data: []byte("module example.com/app\n")},
		"orders/orders.go": {Data: []byte("package orders\n\nimport \"net/http\"\n\nfunc Handle(w http.ResponseWriter, r *http.Request) {}\n\nfunc Write(b []byte) {}\n")},
		"web/web.go": {Data: []byte(`package web

import (
	"net/http"

	"example.com/app/orders"
)

type H struct{}

func (h *H) Plain()   {}
func (h *H) Wrapped() {}
func (h *H) Hidden()  {}

type mw struct{}

func (mw) Wrap(f func()) http.Handler { return nil }

func Register(mux *http.ServeMux, h *H, m mw) {
	mux.HandleFunc("/a", orders.Handle)
	mux.Handle("/b", h.Plain)
	mux.Handle("/c", http.HandlerFunc(h.Wrapped))
	mux.Handle("/d", m.Wrap(h.Hidden))
	mux.HandleFunc("/e", func(orders http.ResponseWriter, r *http.Request) { orders.Write(nil) })
}

func Uses() { orders.Write(nil) }
`)},
	})
	var entries []string
	for _, e := range a.Entries {
		entries = append(entries, e.Symbol+"|"+strings.Join(e.Calls, ","))
	}
	sort.Strings(entries)
	// The literal's entry lists no call: orders there is its parameter.
	want := "example.com/app/orders.Handle| example.com/app/orders.Handle| example.com/app/web.H.Plain|" +
		" example.com/app/web.H.Wrapped| |"
	if got := strings.Join(entries, " "); got != want {
		t.Errorf("entries:\ngot  %s\nwant %s", got, want)
	}
	for _, s := range a.Symbols {
		if s.ID == "example.com/app/web.Register" && s.IO {
			t.Errorf("registering routes counted as input or output")
		}
	}
}

// viewpoint: behaviour
// target: golang.Adapter.Analyze
// A function's calls and the units whose values it uses come sorted, whatever order the body
// names them in.
func TestGoTracedCallsAndUnitsAreSorted(t *testing.T) {
	pkg := func(name string) *fstest.MapFile {
		return &fstest.MapFile{Data: []byte("package " + name + "\n\nvar V = 1\n\nfunc F() {}\n")}
	}
	a := analyzeTree(t, fstest.MapFS{
		"go.mod": {Data: []byte("module example.com/app\n")},
		"d/d.go": pkg("d"), "b/b.go": pkg("b"), "c/c.go": pkg("c"), "a/a.go": pkg("a"),
		"main/main.go": {Data: []byte("package main\n\nimport (\n\t\"example.com/app/a\"\n\t\"example.com/app/b\"\n\t\"example.com/app/c\"\n\t\"example.com/app/d\"\n)\n\n" +
			"func Run() int {\n\td.F()\n\tb.F()\n\tc.F()\n\ta.F()\n\treturn d.V + b.V + c.V + a.V\n}\n")},
	})
	for _, s := range a.Symbols {
		if s.ID != "example.com/app/main.Run" {
			continue
		}
		got := fmt.Sprintf("%v %v", s.Calls, s.Units)
		want := "[example.com/app/a.F example.com/app/b.F example.com/app/c.F example.com/app/d.F] " +
			"[example.com/app/a example.com/app/b example.com/app/c example.com/app/d]"
		if got != want {
			t.Errorf("got  %s\nwant %s", got, want)
		}
		return
	}
	t.Fatal("no symbol main.Run")
}
