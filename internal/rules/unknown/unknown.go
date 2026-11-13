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
		{Name: "a new SQL file whose markers point to two dialects needs review", Base: mod(),
			Head:       mod("db/a.sql", "CREATE TABLE `t` (id INT) ENGINE=InnoDB;\nCREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n"),
			WantReview: []string{"unknown/unread"}},
		{Name: "the dialect set in the configuration makes it read", Base: mod(),
			Head: mod("db/a.sql", "CREATE TABLE t (id int);\nCREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n",
				".hadome.yml", "schema: 1\nsql:\n  databases:\n    - name: main\n      dialect: postgresql\n")},
		{Name: "a new SQL file with a statement that does not parse needs review", Base: mod(),
			Head:       mod("db/a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\nCREATE TABLE t (id int;\n"),
			WantReview: []string{"unknown/unread"}},
		{Name: "a new SQL file whose only syntax error is in the query of a product view is read", Base: mod(),
			Head: mod("db/a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n"+
				"CREATE VIEW sales.daily AS SELECT FROM FROM sales.orders;\n")},
		{Name: "a new SQL file whose only syntax error is in the expression of a BigQuery SQL function is read", Base: mod(),
			Head: mod("bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS (x + * 1) OPTIONS(description = 'adds');\n")},
		{Name: "a new SQL file whose function expression's parentheses do not close needs review", Base: mod(),
			Head:       mod("bq/a.sql", "CREATE FUNCTION ds.f(x INT64) AS ((x + 1) OPTIONS(description = 'adds');\n"),
			WantReview: []string{"unknown/unread"}},
		{Name: "a new SQL file with an error before the AS of a view needs review", Base: mod(),
			Head: mod("db/a.sql", "CREATE FUNCTION f() RETURNS int AS $$ SELECT 1 $$ LANGUAGE sql;\n"+
				"CREATE VIEW sales.daily daily AS SELECT 1;\n"),
			WantReview: []string{"unknown/unread"}},
	}
}

// Find reports every file of a supported language that could not be read.
func (r unread) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, f := range in.Facts.Files {
		if !f.Unread {
			continue
		}
		finding := engine.Finding{
			Rule: r.ID(), File: f.Path, Review: true,
			Message: engine.T("hadome could not read %s", f.Path),
			Why:     engine.T("it does not parse, or this build of hadome has no reader for %s; no rule saw what it does", f.Language),
			Fix:     engine.T("make it parse, or use a build of hadome that reads %s (every prebuilt binary does)", f.Language),
		}
		if f.Language == "sql" {
			finding.Why = engine.T("its dialect is not decided, or one of its statements does not parse in that dialect; no rule saw what it does")
			finding.Fix = engine.T("give its database a dialect in sql.databases, or make every statement parse")
		}
		out = append(out, finding)
	}
	return out, nil
}

// All returns the unknown/ rules.
func All() []engine.Rule { return []engine.Rule{unread{}, unknownImport{}} }

// unknownImport is unknown/import: an import whose destination is decided only at run time, or lies
// in a directory hadome does not read.
type unknownImport struct{}

func (unknownImport) ID() string { return "unknown/import" }

func (unknownImport) About() engine.About {
	return engine.About{Finds: engine.T("a new import whose destination hadome cannot tell: decided only at run time, such as import(name) or Class.forName(name), or inside a directory hadome does not read, such as dist/"),
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
		{Name: "an Ansible role named by a template needs review", Base: mod("site.yml", "- hosts: all\n  roles:\n    - web\n"),
			Head: mod("site.yml", "- hosts: all\n  roles:\n    - web\n    - \"{{ extra_role }}\"\n"), WantReview: []string{"unknown/import"}},
		{Name: "an Ansible role named outright passes", Base: mod("site.yml", "- hosts: all\n  roles:\n    - web\n"),
			Head: mod("site.yml", "- hosts: all\n  roles:\n    - web\n    - community.general.ufw\n")},
	}
}

// Find reports every import of the version whose destination is unknown.
func (r unknownImport) Find(in engine.Input) ([]engine.Finding, error) {
	var out []engine.Finding
	for _, im := range in.Facts.Imports {
		if !im.Unknown {
			continue
		}
		if im.Unread {
			out = append(out, engine.Finding{
				Rule: r.ID(), File: im.Path, Line: im.Line, Anchor: im.Text, Review: true,
				Message: engine.T("this import names %s, inside a directory hadome does not read", im.Text),
				Why:     r.About().Why,
				Fix:     engine.T("import the source that directory is built from; if it must stay, a person approves it"),
			})
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
