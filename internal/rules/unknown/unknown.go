// Package unknown holds the rules about what hadome could not read. Nothing uncertain may block, and
// nothing uncertain may be hidden, so these findings go to a person (exit code 3).
//
// Design: docs/design/adapters-treesitter.md
package unknown

import (
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// unread is unknown/unread.
type unread struct{}

func (unread) ID() string { return "unknown/unread" }

func (unread) About() engine.About {
	return engine.About{Finds: engine.T("a new file of a supported language that hadome could not read"),
		Why:    engine.T("no rule saw what the file does, so nothing it adds was checked"),
		Fix:    engine.T("make it parse, or use a build of hadome that reads its language (every prebuilt binary does)"),
		Review: true}
}

func (unread) Cases() []engine.Case {
	mod := func(files ...string) fstest.MapFS {
		t := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
		for i := 0; i+1 < len(files); i += 2 {
			t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
		}
		return t
	}
	const broken = "package store\n\nfunc Append( {\n"
	return []engine.Case{
		{Name: "a new file that cannot be read needs review", Base: mod("store/a.go", "package store\n"),
			Head: mod("store/a.go", "package store\n", "store/b.go", broken), WantReview: []string{"unknown/unread"}},
		{Name: "one already unread in base does not", Base: mod("store/b.go", broken), Head: mod("store/b.go", broken)},
		{Name: "a file of no supported language is not unread", Base: mod(), Head: mod("notes.txt", "hello")},
	}
}

// Find reports every file of a supported language that could not be read.
func (r unread) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, f := range in.Facts.Files {
		if !f.Unread {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: f.Path, Review: true,
			Message: engine.T("hadome could not read %s", f.Path),
			Why:     engine.T("it does not parse, or this build of hadome has no reader for %s; no rule saw what it does", f.Language),
			Fix:     engine.T("make it parse, or use a build of hadome that reads %s (every prebuilt binary does)", f.Language),
		})
	}
	return out, nil
}

// All returns the unknown/ rules.
func All() []engine.Rule { return []engine.Rule{unread{}, unknownImport{}} }

// unknownImport is unknown/import: an import whose destination is decided only at run time.
type unknownImport struct{}

func (unknownImport) ID() string { return "unknown/import" }

func (unknownImport) About() engine.About {
	return engine.About{Finds: engine.T("a new import whose destination is decided only at run time, such as import(name) or Class.forName(name)"),
		Why:    engine.T("no rule can tell what it reaches, so dependencies and purity are judged without it"),
		Fix:    engine.T("import the destination by name; if it must stay dynamic, a person approves it"),
		Review: true}
}

// Cases holds what every build can run; a build with the tree-sitter languages adds theirs.
func (unknownImport) Cases() []engine.Case { return append(goImportCases(), languageImportCases...) }

// languageImportCases are the cases in languages other than Go, set by the build that reads them.
var languageImportCases []engine.Case

func goImportCases() []engine.Case {
	mod := func(files ...string) fstest.MapFS {
		t := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
		for i := 0; i+1 < len(files); i += 2 {
			t[files[i]] = &fstest.MapFile{Data: []byte(files[i+1])}
		}
		return t
	}
	return []engine.Case{
		{Name: "a Go import is always named, so it is never unknown", Base: mod("a/a.go", "package a\n"),
			Head: mod("a/a.go", "package a\n\nimport \"example.com/app/b\"\n\nvar _ = b.X\n", "b/b.go", "package b\n\nvar X = 1\n")},
	}
}

// Find reports every import of the version whose destination is unknown.
func (r unknownImport) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, im := range in.Facts.Imports {
		if !im.Unknown {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: im.Path, Line: im.Line, Anchor: im.Text, Review: true,
			Message: engine.T("the destination of this import is decided only at run time: %s", im.Text),
			Why:     r.About().Why, Fix: r.About().Fix,
		})
	}
	return out, nil
}
