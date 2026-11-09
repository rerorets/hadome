package comments_test

import (
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/comments"
)

// viewpoint: external contract
// target: comments/broken-reference through engine.Engine.Check
// A reference to a missing file passes exactly when git, given only the repository's .gitignore
// files, would ignore that path. The expected answers are git's documented pattern semantics.
func TestBrokenReferenceFollowsGitignorePatterns(t *testing.T) {
	for _, tc := range []struct {
		name     string
		ignores  map[string]string // .gitignore files by path
		ref      string
		reported bool
	}{
		{"no .gitignore", nil, "notes/rows.md", true},
		{"a directory pattern", map[string]string{".gitignore": "notes/\n"}, "notes/rows.md", false},
		{"a directory pattern does not match a file of that name", map[string]string{".gitignore": "rows.md/\n"}, "rows.md", true},
		{"a name matches at any depth", map[string]string{".gitignore": "notes\n"}, "store/notes/rows.md", false},
		{"a leading slash anchors to the root", map[string]string{".gitignore": "/notes\n"}, "store/notes/rows.md", true},
		{"a slash in the middle anchors to the root", map[string]string{".gitignore": "store/notes\n"}, "app/store/notes/rows.md", true},
		{"a star does not cross a slash", map[string]string{".gitignore": "notes/*.md\n"}, "notes/old/rows.md", true},
		{"a leading ** matches any directories", map[string]string{".gitignore": "**/drafts/*.md\n"}, "a/b/drafts/rows.md", false},
		{"a middle ** matches zero directories", map[string]string{".gitignore": "notes/**/rows.md\n"}, "notes/rows.md", false},
		{"a trailing ** matches everything inside", map[string]string{".gitignore": "notes/**\n"}, "notes/a/b/rows.md", false},
		{"a later negation re-includes", map[string]string{".gitignore": "*.md\n!rows.md\n"}, "notes/rows.md", true},
		{"a file in an ignored directory cannot be re-included", map[string]string{".gitignore": "notes/\n!notes/rows.md\n"}, "notes/rows.md", false},
		{"a deeper .gitignore overrides the root's", map[string]string{".gitignore": "*.md\n", "notes/.gitignore": "!rows.md\n"}, "notes/rows.md", true},
		{"a nested .gitignore is relative to its directory", map[string]string{"store/.gitignore": "/notes/\n"}, "store/notes/rows.md", false},
		{"a comment line is not a pattern", map[string]string{".gitignore": "# notes/\n"}, "notes/rows.md", true},
		{"a character class", map[string]string{".gitignore": "draft[0-9].md\n"}, "draft3.md", false},
		{"a negated character class", map[string]string{".gitignore": "draft[!0-9].md\n"}, "draft3.md", true},
		{"a negated character class matches what it leaves out", map[string]string{".gitignore": "draft[!0-9].md\n"}, "draftx.md", false},
		{"a question mark matches one character", map[string]string{".gitignore": "draft?.md\n"}, "draft3.md", false},
		{"an escaped star is a literal star", map[string]string{".gitignore": "notes\\*/\n"}, "notesx/rows.md", true},
		{"an escaped ordinary character is itself", map[string]string{".gitignore": "no\\tes/\n"}, "notes/rows.md", false},
		{"a trailing backslash never matches", map[string]string{".gitignore": "rows.md\\\n"}, "rows.md", true},
		{"an escaped backslash before a lone one never matches", map[string]string{".gitignore": "rows.md\\\\\\\n"}, "rows.md", true},
		{"an unclosed bracket is literal", map[string]string{".gitignore": "draft[.md\n"}, "draft.md", true},
		{"an unreadable line does not stop the others", map[string]string{".gitignore": "draft[].md\nnotes/\n"}, "notes/rows.md", false},
		{"a lone slash is not a pattern", map[string]string{".gitignore": "/\n"}, "notes/rows.md", true},
		{"trailing spaces are dropped", map[string]string{".gitignore": "notes/  \n"}, "notes/rows.md", false},
		{"a carriage return is dropped", map[string]string{".gitignore": "notes/\r\n"}, "notes/rows.md", false},
		{"a reference above the root is never ignored", map[string]string{".gitignore": "*.md\n"}, "../../../rows.md", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := func(comment string) fstest.MapFS {
				m := fstest.MapFS{
					"go.mod":         {Data: []byte("module example.com/app\n")},
					".hadome.yml":    {Data: []byte("schema: 1\n")},
					"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n" + comment)},
				}
				for p, body := range tc.ignores {
					m[p] = &fstest.MapFile{Data: []byte(body)}
				}
				return m
			}
			e := &engine.Engine{Analyzer: adapter.New(), Rules: comments.All()}
			r, err := e.Check(trees{base: tree(""), head: tree("\n// See " + tc.ref + ".\n")})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(r.Regressions) == 1; got != tc.reported || len(r.Regressions) > 1 {
				t.Errorf("reported = %v, want %v: %+v", len(r.Regressions), tc.reported, r.Regressions)
			}
		})
	}
}

// viewpoint: behaviour
// target: comments/broken-reference through engine.Engine.Check
// A reference such as README/CLAUDE.md that is not a path is read as file names joined by "or". It
// passes only when every part names a file somewhere (a part without .md also by the name with .md
// added), no part is empty, the first part is not a directory at the root, and it does not start
// with ./, ../ or /.
func TestBrokenReferenceReadsFileNamesJoinedBySlash(t *testing.T) {
	for _, tc := range []struct {
		name     string
		files    []string
		ref      string
		reported bool
	}{
		{"every part exists", []string{"README.md", "docs/CLAUDE.md"}, "README/CLAUDE.md", false},
		{"a part without .md exists as it is", []string{"README", "CLAUDE.md"}, "README/CLAUDE.md", false},
		{"three parts", []string{"README.md", "AGENTS.md", "CLAUDE.md"}, "README/AGENTS/CLAUDE.md", false},
		{"a part does not exist", []string{"README.md"}, "README/CLAUDE.md", true},
		{"a part ending in .md is not given another .md", []string{"README.md.md", "CLAUDE.md"}, "README.md/CLAUDE.md", true},
		{"an empty part", []string{"README.md", "CLAUDE.md", ".md"}, "README//CLAUDE.md", true},
		{"a directory at the root", []string{"docs/rows.md", "stroe.md", "docs.md"}, "docs/stroe.md", true},
		{"a reference from the file", []string{"README.md", "CLAUDE.md"}, "./README/CLAUDE.md", true},
		{"a reference from the parent", []string{"README.md", "CLAUDE.md"}, "../README/CLAUDE.md", true},
		{"a reference from the root", []string{"README.md", "CLAUDE.md"}, "/README/CLAUDE.md", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tree := func(comment string) fstest.MapFS {
				m := fstest.MapFS{
					"go.mod":         {Data: []byte("module example.com/app\n")},
					".hadome.yml":    {Data: []byte("schema: 1\n")},
					"store/store.go": {Data: []byte("// Package store keeps rows.\npackage store\n" + comment)},
				}
				for _, p := range tc.files {
					m[p] = &fstest.MapFile{Data: []byte("")}
				}
				return m
			}
			e := &engine.Engine{Analyzer: adapter.New(), Rules: comments.All()}
			r, err := e.Check(trees{base: tree(""), head: tree("\n// See " + tc.ref + ".\n")})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(r.Regressions) == 1; got != tc.reported || len(r.Regressions) > 1 {
				t.Errorf("reported = %v, want %v: %+v", len(r.Regressions), tc.reported, r.Regressions)
			}
		})
	}
}
