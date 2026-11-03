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
			Message: "hadome could not read " + f.Path,
			Why:     "it does not parse, or this build of hadome has no reader for " + f.Language + "; no rule saw what it does",
			Fix:     "make it parse, or use a build of hadome that reads " + f.Language + " (every prebuilt binary does)",
		})
	}
	return out, nil
}

// All returns the unknown/ rules.
func All() []engine.Rule { return []engine.Rule{unread{}} }
