// Package size holds the size/ rules: upper limits on files, functions and interfaces of product code.
//
// Design: docs/design/rules-size.md
package size

import (
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// measured is one thing a rule measures: where it is, what it is called and how big it is. A
// function nested in the symbol name has nested set: it is reported under that symbol, at its own
// line, and matched against base apart from the symbol's own finding. A whole file has whole set:
// it is reported at its first line over the limit, which depends on the limit and not on the file.
type measured struct {
	file, name    string
	line, size    int
	nested, whole bool
}

// limitRule reports everything over one limit. Each finding carries its size as the Value, so a
// violation that grows is a regression even though it was already in base.
type limitRule struct {
	key     string // the configuration key under rules.size, and the rule's name
	what    string // "lines", "parameters", …
	fix     string
	measure func(a *facts.Analysis) []measured
	cases   func() []engine.Case
}

func (r limitRule) ID() string           { return "size/" + r.key }
func (r limitRule) Cases() []engine.Case { return r.cases() }
func (r limitRule) About() engine.About {
	return engine.About{Finds: engine.T("product code over the %s limit (%s); an existing one that grows is a regression too", r.key, engine.T(r.what)),
		Why:  engine.T("past the limit, the code no longer fits in one reading, and changes to it are made without understanding all of it"),
		Fix:  engine.T("%s; raising the limit (rules.size.%s) is a change a person reviews", engine.T(r.fix), r.key),
		Keys: []string{"rules.size." + r.key}}
}

func (r limitRule) Find(in engine.Input) ([]engine.Finding, error) {
	limit := in.Config.Size[r.key]
	names := in.Facts.Names()
	var out []engine.Finding
	for _, m := range r.measure(in.Facts) {
		if m.size <= limit {
			continue
		}
		f := engine.Finding{
			Rule: r.ID(), File: m.file, Line: m.line, Anchor: m.name, Value: m.size,
			Message: engine.T("%s: %s %d, limit %d", display(m, names), engine.T(r.what), m.size, limit),
			Why:     r.About().Why,
			Fix:     r.About().Fix,
		}
		if m.whole {
			f.Line, f.Whole = limit+1, true
		}
		if m.nested {
			f.Anchor += "\x00nested"
			f.Message = engine.T("the function in %s at line %d: %s %d, limit %d", display(m, names), m.line, engine.T(r.what), m.size, limit)
		}
		out = append(out, f)
	}
	return out, nil
}

func display(m measured, names facts.Names) string {
	if m.name == "" {
		return m.file
	}
	return names.Short(m.name)
}

func files(a *facts.Analysis) []measured {
	var out []measured
	for _, f := range a.Files {
		if f.Role == facts.RoleSource {
			out = append(out, measured{file: f.Path, size: f.Lines, whole: true})
		}
	}
	return out
}

func symbols(kinds []facts.SymbolKind, size func(facts.Symbol) int) func(a *facts.Analysis) []measured {
	return func(a *facts.Analysis) []measured {
		var out []measured
		for _, s := range a.Symbols {
			for _, k := range kinds {
				if s.Kind == k {
					out = append(out, measured{file: s.Path, name: s.ID, line: s.Line, size: size(s)})
				}
			}
		}
		return out
	}
}

var functions = []facts.SymbolKind{facts.SymbolFunction, facts.SymbolMethod}

// complexities measures each function and method, and on their own each function nested in one.
func complexities(a *facts.Analysis) []measured {
	out := symbols(functions, func(s facts.Symbol) int { return s.Complexity })(a)
	for _, s := range a.Symbols {
		if s.Kind != facts.SymbolFunction && s.Kind != facts.SymbolMethod {
			continue
		}
		for _, n := range s.Nested {
			out = append(out, measured{file: s.Path, name: s.ID, line: n.Line, size: n.Complexity, nested: true})
		}
	}
	return out
}

// All returns the size/ rules.
func All() []engine.Rule {
	return []engine.Rule{
		limitRule{key: "file-lines", what: "lines", measure: files, cases: fileCases,
			fix: "split the file along a line it already has: one file per type or per concern"},
		limitRule{key: "function-lines", what: "lines", cases: functionCases,
			measure: symbols(functions, func(s facts.Symbol) int { return s.EndLine - s.Line + 1 }),
			fix:     "extract the steps of the function into functions of their own"},
		limitRule{key: "params", what: "parameters", cases: paramCases,
			measure: symbols(functions, func(s facts.Symbol) int { return s.Params }),
			fix:     "group parameters that travel together into a type"},
		limitRule{key: "complexity", what: "cyclomatic complexity", cases: complexityCases, measure: complexities,
			fix: "move each branch's work into a function, or replace a chain of cases with a table"},
		limitRule{key: "interface-methods", what: "methods", cases: interfaceCases,
			measure: symbols([]facts.SymbolKind{facts.SymbolInterface}, func(s facts.Symbol) int { return s.Methods }),
			fix:     "split the interface by who uses it; each caller should need only what it calls"},
	}
}
