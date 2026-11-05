// Package comments holds the comments/ rules: TODOs say when, public code says what it is, and
// references to documents lead somewhere.
//
// Design: docs/design/rules-comments.md
package comments

import (
	"io/fs"
	"path"
	"regexp"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// line is one line of a comment in a file that is not generated.
type line struct {
	path string
	n    int
	text string
}

// lines splits the comments of files that are not generated into lines, without the leading
// spaces and the asterisks of block comments. Directives are not prose and are left out.
func lines(a *facts.Analysis) []line {
	generated := map[string]bool{}
	for _, f := range a.Files {
		generated[f.Path] = f.Role == facts.RoleGenerated
	}
	var out []line
	for _, c := range a.Comments {
		if generated[c.Path] || c.Directive {
			continue
		}
		for i, t := range strings.Split(c.Text, "\n") {
			t = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(t), "*"))
			out = append(out, line{path: c.Path, n: c.Line + i, text: t})
		}
	}
	return out
}

// todoFormat is comments/todo-format.
type todoFormat struct{}

func (todoFormat) ID() string           { return "comments/todo-format" }
func (todoFormat) Cases() []engine.Case { return todoCases() }
func (todoFormat) About() engine.About {
	return engine.About{Finds: engine.T("a new TODO, FIXME or XXX comment that does not match the configured form"),
		Why:  engine.T("a note to do something later that does not say when is never done; it stays as noise and misleads readers about what is finished"),
		Fix:  engine.T("write it in the configured form (for example TODO(stage 2): …), do it now, or delete it"),
		Keys: []string{"rules.comments.todo"}}
}

var todoWord = regexp.MustCompile(`^(TODO|FIXME|XXX)\b`)

// Find reports TODO, FIXME and XXX lines that do not have the configured form.
func (r todoFormat) Find(in engine.Input) ([]engine.Finding, error) {
	form, err := regexp.Compile(in.Config.TodoForm)
	if err != nil {
		return nil, err // validation already rejects this; a check that cannot run never passes
	}
	var out []engine.Finding
	for _, l := range lines(in.Facts) {
		if !todoWord.MatchString(l.text) || form.MatchString(l.text) {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: l.path, Line: l.n, Anchor: strings.Join(strings.Fields(l.text), " "),
			Message: engine.T("TODO without the required form: %s", l.text),
			Why:     r.About().Why,
			Fix:     engine.T("write it in the form %s (for example TODO(stage 2): …), do it now, or delete it", in.Config.TodoForm),
		})
	}
	return out, nil
}

// missingDoc is comments/missing-doc.
type missingDoc struct{}

func (missingDoc) ID() string           { return "comments/missing-doc" }
func (missingDoc) Cases() []engine.Case { return docCases() }
func (missingDoc) About() engine.About {
	return engine.About{Finds: engine.T("a new exported function, method or type of product code without a doc comment, " +
		"or a new Terraform variable or output without a description"),
		Why: engine.T("callers read the doc comment instead of the body; without it, every caller reads the implementation and guesses"),
		Fix: engine.T("write one sentence that starts with the name and says what it does or is, and what a caller would otherwise get wrong")}
}

// Find reports exported symbols of product code without a doc comment.
func (r missingDoc) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, s := range in.Facts.Symbols {
		if !s.Exported || s.Doc {
			continue
		}
		name := s.ID[strings.LastIndex(s.ID, "/")+1:]
		msg := engine.T("exported %s %s has no doc comment", string(s.Kind), name)
		if s.Kind == facts.SymbolValue { // a Terraform variable or output: its description is its doc
			msg = engine.T("value %s has no description", name)
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: s.Path, Line: s.Line, Anchor: s.ID,
			Message: msg,
			Why:     r.About().Why,
			Fix:     r.About().Fix,
		})
	}
	return out, nil
}

// brokenReference is comments/broken-reference.
type brokenReference struct{}

func (brokenReference) ID() string           { return "comments/broken-reference" }
func (brokenReference) Cases() []engine.Case { return referenceCases() }
func (brokenReference) About() engine.About {
	return engine.About{Finds: engine.T("a comment that names a Markdown file the version does not have"),
		Why: engine.T("a pointer to a document promises that the reasoning is written down; a broken one sends the reader nowhere"),
		Fix: engine.T("point at the document's current path, or remove the reference")}
}

// reference is a word ending in .md, optionally followed by a section.
var reference = regexp.MustCompile("(?:^|[\\s(\\[`\"'])((?:\\.{1,2}/)*[A-Za-z0-9_./-]*[A-Za-z0-9_-]\\.md)(?:#[A-Za-z0-9_-]*)?")

// Find reports references to Markdown files that do not exist in the version.
func (r brokenReference) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, l := range lines(in.Facts) {
		for _, m := range reference.FindAllStringSubmatch(l.text, -1) {
			ref := m[1]
			target := strings.TrimPrefix(path.Clean(ref), "/")
			if strings.HasPrefix(ref, "./") || strings.HasPrefix(ref, "../") {
				target = path.Join(path.Dir(l.path), ref)
			}
			if _, err := fs.Stat(in.Version.FS, target); err == nil {
				continue
			}
			out = append(out, engine.Finding{
				Rule: r.ID(), File: l.path, Line: l.n, Anchor: ref,
				Message: engine.T("comment refers to %s, which does not exist", ref),
				Why:     r.About().Why,
				Fix:     r.About().Fix,
			})
		}
	}
	return out, nil
}

// All returns the comments/ rules.
func All() []engine.Rule {
	return []engine.Rule{todoFormat{}, missingDoc{}, brokenReference{}}
}
