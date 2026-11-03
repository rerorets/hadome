// Package size holds the size/ rules: upper limits on files, functions and interfaces of product code.
//
// Design: docs/design/rules-size.md
package size

import (
	"fmt"
	"strings"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// measured is one thing a rule measures: where it is, what it is called and how big it is.
type measured struct {
	file, name string
	line, size int
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

func (r limitRule) Find(in engine.Input) ([]engine.Finding, error) {
	limit := in.Config.Size[r.key]
	var out []engine.Finding
	for _, m := range r.measure(in.Facts) {
		if m.size <= limit {
			continue
		}
		out = append(out, engine.Finding{
			Rule: r.ID(), File: m.file, Line: m.line, Anchor: m.name, Value: m.size,
			Message: fmt.Sprintf("%s: %s %d, limit %d", display(m), r.what, m.size, limit),
			Why:     "past the limit, the code no longer fits in one reading, and changes to it are made without understanding all of it",
			Fix:     r.fix + "; raising the limit (rules.size." + r.key + ") is a change a person reviews",
		})
	}
	return out, nil
}

func display(m measured) string {
	if m.name == "" {
		return m.file
	}
	return m.name[strings.LastIndex(m.name, "/")+1:]
}

func files(a *facts.Analysis) []measured {
	var out []measured
	for _, f := range a.Files {
		if f.Role == facts.RoleSource {
			out = append(out, measured{file: f.Path, size: f.Lines})
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
		limitRule{key: "complexity", what: "cyclomatic complexity", cases: complexityCases,
			measure: symbols(functions, func(s facts.Symbol) int { return s.Complexity }),
			fix:     "move each branch's work into a function, or replace a chain of cases with a table"},
		limitRule{key: "interface-methods", what: "methods", cases: interfaceCases,
			measure: symbols([]facts.SymbolKind{facts.SymbolInterface}, func(s facts.Symbol) int { return s.Methods }),
			fix:     "split the interface by who uses it; each caller should need only what it calls"},
	}
}
