// Package comments holds the comments/ rules: TODOs say when, public code says what it is, and
// references to documents lead somewhere.
//
// Design: docs/design/rules-comments.md
package comments

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/rerorets/hadome/internal/config"
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
		Fix:  engine.T("write it in the configured form (rules.comments.todo), do it now, or delete it"),
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
			Fix:     todoFix(form, in.Config.TodoForm),
		})
	}
	return out, nil
}

// todoExamples are TODOs in common forms. The fix shows the first one the configured form accepts,
// so the example never contradicts the form it illustrates.
var todoExamples = []string{
	"TODO(stage 2): handle errors", "TODO(#123): handle errors", "TODO(ABC-123): handle errors",
	"TODO(2030-01-31): handle errors", "TODO(alice): handle errors", "TODO: handle errors",
}

// todoFix says how to fix a TODO under the configured form, with an example when one of the
// common forms matches it. A form none of them matches is shown alone rather than with an example
// that would itself fail.
func todoFix(form *regexp.Regexp, source string) engine.Text {
	for _, ex := range todoExamples {
		if form.MatchString(ex) {
			return engine.T("write it in the form %s (for example %s), do it now, or delete it", source, ex)
		}
	}
	return engine.T("write it in the form %s, do it now, or delete it", source)
}

// missingDoc is comments/missing-doc.
type missingDoc struct{}

func (missingDoc) ID() string           { return "comments/missing-doc" }
func (missingDoc) Cases() []engine.Case { return docCases() }
func (missingDoc) About() engine.About {
	return engine.About{Finds: engine.T("a new exported function, method or type of product code without a doc comment, " +
		"or a new Terraform variable or output or Ansible role default without a description"),
		Why: engine.T("callers read the doc comment instead of the body; without it, every caller reads the implementation and guesses"),
		Fix: engine.T("write one sentence that starts with the name and says what it does or is, and what a caller would otherwise get wrong; " +
			"in TypeScript, JavaScript, Java and Kotlin write it as /** */, since a // line comment is not a doc comment"),
		Keys: []string{"rules.comments.entry-points"}}
}

var (
	valueWhy = engine.T("whoever sets or reads the value reads its description instead of the code that uses it; " +
		"without one, each of them reads that code and guesses what the value is for")
	valueFix = engine.T("add a description that says what the value is for and what a caller would otherwise get wrong: " +
		"Terraform's description argument, or the variable's description in the role's meta/argument_specs.yml")
)

// Find reports exported symbols of product code without a doc comment, other than the configured
// framework entry points.
func (r missingDoc) Find(in engine.Input) ([]engine.Finding, error) {
	units := map[string]string{}
	for _, f := range in.Facts.Files {
		units[f.Path] = f.Unit
	}
	var out []engine.Finding
	for _, s := range in.Facts.Symbols {
		if !s.Exported || s.Doc || s.DocElsewhere || entryPoint(in.Config.EntryPoints, s, units[s.Path]) {
			continue
		}
		name := facts.ShortName(s.ID)
		f := engine.Finding{Rule: r.ID(), File: s.Path, Line: s.Line, Anchor: s.ID,
			Message: engine.T("exported %s %s has no doc comment", string(s.Kind), name), Why: r.About().Why, Fix: r.About().Fix}
		if s.Kind == facts.SymbolValue { // a Terraform variable or output, an Ansible default: a description is its doc
			f.Message, f.Why, f.Fix = engine.T("value %s has no description", name), valueWhy, valueFix
		}
		out = append(out, f)
	}
	return out, nil
}

// entryPoint reports whether a configured entry point covers the symbol. Its name is its ID
// without the unit: load, or Store.Append for a method. An ID that does not start with its file's
// unit (Python's start with the module, tests/helpers.build) loses its last path element's first
// part instead.
func entryPoint(eps []config.EntryPoint, s facts.Symbol, unit string) bool {
	name := strings.TrimPrefix(s.ID, unit+".")
	if unit == "" || name == s.ID {
		name = facts.ShortName(s.ID)
		name = name[strings.Index(name, ".")+1:]
	}
	for _, e := range eps {
		if e.Matches(name, s.Path) {
			return true
		}
	}
	return false
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

// reference is a word ending in .md, optionally followed by a section. It starts a line or follows
// a space, a bracket, a quote, or a Japanese separator such as ・ or （.
var reference = regexp.MustCompile("(?:^|[\\s(\\[`\"'・、（「『【：　])((?:\\.{1,2}/)*[A-Za-z0-9_./-]*[A-Za-z0-9_-]\\.md)(?:#[A-Za-z0-9_-]*)?")

// Find reports references to Markdown files that do not exist in the version, unless the version's
// .gitignore files ignore the path.
func (r brokenReference) Find(in engine.Input) ([]engine.Finding, error) {
	var names map[string]bool // the version's file names, read at the first bare name that is not at the root
	var ignore *gitignore     // the version's .gitignore files, read at the first missing target
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
			if !strings.Contains(ref, "/") {
				if names == nil {
					var err error
					if names, err = fileNames(in.Version.FS); err != nil {
						return nil, err
					}
				}
				if names[ref] {
					continue
				}
			}
			if ignore == nil {
				ignore = newGitignore(in.Version.FS)
			}
			if ignore.ignored(target) {
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

// fileNames returns the names of the files outside the directories the adapters skip.
func fileNames(fsys fs.FS) (map[string]bool, error) {
	names := map[string]bool{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && skipped(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		names[d.Name()] = true
		return nil
	})
	return names, err
}

// skippedDirs repeats internal/adapter/walk.SkippedDirs, which rules may not import. A test in
// internal/adapter fails when the two differ.
var skippedDirs = []string{"vendor", "testdata", "node_modules", "target", "build", "dist", "__pycache__"}

// SkippedDirs lists the directories, besides those starting with a dot, whose files a reference by
// file name only never finds.
func SkippedDirs() []string { return slices.Clone(skippedDirs) }

func skipped(dir string) bool {
	return slices.Contains(skippedDirs, dir) || strings.HasPrefix(dir, ".")
}

// All returns the comments/ rules.
func All() []engine.Rule {
	return []engine.Rule{todoFormat{}, missingDoc{}, brokenReference{}}
}
