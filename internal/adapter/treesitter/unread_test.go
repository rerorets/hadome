//go:build !cgo

package treesitter

import (
	"fmt"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
)

// viewpoint: behaviour
// target: treesitter.Languages without cgo
// A build without cgo still has one reader per language, in the fixed order, and each claims its
// own language's files and no other's.
func TestUnreadReadersClaimTheirLanguagesFiles(t *testing.T) {
	files := []string{"a.ts", "a.tsx", "a.js", "a.py", "a.rs", "a.java", "a.kt", "a.kts", "a.go", "README.md"}
	var got []string
	for _, l := range Languages() {
		var claimed []string
		for _, p := range files {
			if l.Claims(p) {
				claimed = append(claimed, p)
			}
		}
		got = append(got, l.Name()+": "+strings.Join(claimed, " "))
	}
	want := strings.Join([]string{
		"typescript: a.ts a.tsx a.js",
		"python: a.py",
		"rust: a.rs",
		"java: a.java",
		"kotlin: a.kt a.kts",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// viewpoint: behaviour
// target: treesitter.Languages without cgo
// Without cgo a language's files are listed, each with its language, the role other, its lines,
// and marked unread, so a check says they were not read instead of passing over them. A file that
// cannot be read is an error.
func TestUnreadReadersListFilesAsUnread(t *testing.T) {
	tree := fstest.MapFS{
		"src/calc.py":  {Data: []byte("def add(a, b):\n    return a + b\n")},
		"src/empty.py": {Data: []byte("")},
	}
	python := Languages()[1]
	a, err := python.Analyze(reuse.Lone(tree), config.Analysis{}, []string{"src/calc.py", "src/empty.py"})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range a.Files {
		got = append(got, fmt.Sprintf("%s %s %s lines=%d unread=%t", f.Path, f.Language, f.Role, f.Lines, f.Unread))
	}
	want := "src/calc.py python other lines=2 unread=true\nsrc/empty.py python other lines=0 unread=true"
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
	if len(a.Symbols)+len(a.Tests)+len(a.Imports) != 0 {
		t.Errorf("want nothing read from unread files, got %+v", a)
	}
	if _, err := python.Analyze(reuse.Lone(tree), config.Analysis{}, []string{"src/gone.py"}); err == nil {
		t.Error("want an error for a file that cannot be read")
	}
}
