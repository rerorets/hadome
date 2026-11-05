// Package terraform is the Terraform language adapter. It reads Terraform with HCL's own parser,
// in pure Go, so it reads in builds without cgo too. Terraform has no functions or classes: the
// module's variables and outputs are its symbols, each run block of a test file is a test.
//
// Design: docs/design/adapters-iac.md
package terraform

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"
	"github.com/zclconf/go-cty/cty"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Adapter is the Terraform adapter.
type Adapter struct{}

const name = "terraform"

// Name is the language's name in configuration.
func (Adapter) Name() string { return name }

// Claims reports whether the file is Terraform: configuration, a test file, or a variables file.
func (Adapter) Claims(p string) bool {
	return strings.HasSuffix(p, ".tf") || strings.HasSuffix(p, ".tftest.hcl") || strings.HasSuffix(p, ".tfvars")
}

// Analyze reads the Terraform files of the version. A file HCL cannot parse is listed as unread and
// yields nothing else, as in the Go adapter. Files matching files.tests are not made tests: Terraform
// itself decides what a test is, by the .tftest.hcl suffix.
func (Adapter) Analyze(v engine.Version, c config.Config, paths []string) (*facts.Analysis, error) {
	a := &facts.Analysis{}
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		dir := path.Dir(p)
		f := facts.File{Path: p, Language: name, Lines: strings.Count(string(src), "\n") + 1, Unit: dir, UnitPath: dir}
		if strings.HasSuffix(p, ".tfvars") { // values only: nothing to read
			f.Role, f.Unit, f.UnitPath = facts.RoleOther, "", ""
			a.Files = append(a.Files, f)
			continue
		}
		parsed, diags := hclsyntax.ParseConfig(src, p, hcl.InitialPos)
		body, ok := parsed.Body.(*hclsyntax.Body)
		if diags.HasErrors() || !ok {
			a.Files = append(a.Files, facts.File{Path: p, Language: name, Role: facts.RoleOther, Lines: f.Lines, Unread: true})
			continue
		}
		lc := lexComments(p, src)
		cs := make([]facts.Comment, len(lc))
		for i, c := range lc {
			cs[i] = c.Comment
		}
		test := strings.HasSuffix(p, ".tftest.hcl")
		switch {
		case test:
			f.Role = facts.RoleTest
		case config.MatchesAny(c.GeneratedFiles, p) || generated(cs):
			f.Role = facts.RoleGenerated
		default:
			f.Role = facts.RoleSource
		}
		a.Files = append(a.Files, f)
		a.Comments = append(a.Comments, cs...)
		if test {
			a.Tests = append(a.Tests, tests(p, src, body, lc)...)
			continue
		}
		a.Imports = append(a.Imports, imports(p, body, c.IO)...)
		if f.Role == facts.RoleSource {
			a.Symbols = append(a.Symbols, symbols(p, body)...)
		}
	}
	return a, nil
}

// symbolID names a variable or output the way Terraform refers to it inside the module (var.region,
// output.region), so that a variable and an output of the same name stay apart.
func symbolID(module, kind, name string) string { return module + "." + kind + "." + name }

// symbols lists the module's entrances and exits: every variable and output, always exported, with
// a description counting as their doc.
func symbols(p string, body *hclsyntax.Body) []facts.Symbol {
	var out []facts.Symbol
	for _, b := range body.Blocks {
		kind := map[string]string{"variable": "var", "output": "output"}[b.Type]
		if kind == "" || len(b.Labels) != 1 {
			continue
		}
		r := b.Range()
		out = append(out, facts.Symbol{ID: symbolID(path.Dir(p), kind, b.Labels[0]), Kind: facts.SymbolValue, Path: p,
			Line: r.Start.Line, EndLine: r.End.Line, Exported: true, Doc: described(b.Body)})
	}
	return out
}

// described reports whether a block has a description that is not empty.
func described(body *hclsyntax.Body) bool {
	attr, ok := body.Attributes["description"]
	if !ok {
		return false
	}
	if s, ok := literal(attr.Expr); ok {
		return strings.TrimSpace(s) != ""
	}
	return true // an expression: something is written there
}

// literal returns the value of a string written without interpolation.
func literal(e hclsyntax.Expression) (string, bool) {
	if len(e.Variables()) > 0 {
		return "", false
	}
	v, diags := e.Value(nil)
	if diags.HasErrors() || !v.IsKnown() || v.IsNull() || v.Type() != cty.String {
		return "", false
	}
	return v.AsString(), true
}

// imports lists the modules a file calls and the providers it requires. A provider is how a module
// reaches outside, so its import performs input and output; a module's does when configuration lists
// it in rules.tests.io.
func imports(p string, body *hclsyntax.Body, io []string) []facts.Import {
	dir := path.Dir(p)
	var out []facts.Import
	add := func(line int, source string, provider bool) {
		im := facts.Import{Path: p, Line: line, FromUnit: dir, Text: source, IO: provider}
		if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
			im.ToUnit, im.Internal = path.Join(dir, source), true
		} else {
			im.ToUnit = external(source)
		}
		im.IO = im.IO || slices.ContainsFunc(io, func(e string) bool { return im.ToUnit == e || strings.HasPrefix(im.ToUnit, e+"/") })
		out = append(out, im)
	}
	for _, b := range body.Blocks {
		switch b.Type {
		case "module":
			if attr, ok := b.Body.Attributes["source"]; ok {
				if s, ok := literal(attr.Expr); ok {
					add(attr.SrcRange.Start.Line, s, false)
				}
			}
		case "terraform":
			for _, src := range providerSources(b.Body) {
				add(src.Range().Start.Line, mustLiteral(src), true)
			}
		}
	}
	return out
}

// providerSources returns the source of every provider a terraform block requires.
func providerSources(body *hclsyntax.Body) []hclsyntax.Expression {
	var out []hclsyntax.Expression
	for _, rp := range body.Blocks {
		if rp.Type != "required_providers" {
			continue
		}
		for _, attr := range sortedAttributes(rp.Body) {
			obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
			if !ok { // the old form, a version alone, names no source
				continue
			}
			for _, item := range obj.Items {
				if _, ok := literal(item.ValueExpr); ok && hcl.ExprAsKeyword(item.KeyExpr) == "source" {
					out = append(out, item.ValueExpr)
				}
			}
		}
	}
	return out
}

func mustLiteral(e hclsyntax.Expression) string {
	s, _ := literal(e)
	return s
}

// external is the name of a module or provider outside the repository: the address as written,
// without the query (?ref=v1.2) and the subdirectory after // that pick a version and a part of it.
func external(source string) string {
	s, _, _ := strings.Cut(source, "?")
	for i := 0; i+1 < len(s); i++ {
		if s[i] == '/' && s[i+1] == '/' && (i == 0 || s[i-1] != ':') {
			return s[:i]
		}
	}
	return s
}

// sortedAttributes returns a body's attributes in source order; HCL keeps them in a map.
func sortedAttributes(body *hclsyntax.Body) []*hclsyntax.Attribute {
	var out []*hclsyntax.Attribute
	for _, a := range body.Attributes {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].SrcRange.Start.Byte < out[j].SrcRange.Start.Byte })
	return out
}

var generatedMark = regexp.MustCompile(`(?i)(@generated|code generated .* do not edit|auto-generated)`)

// generated reports whether one of the first comments marks the file as generated.
func generated(cs []facts.Comment) bool {
	for i, c := range cs {
		if i > 2 {
			break
		}
		if generatedMark.MatchString(c.Text) {
			return true
		}
	}
	return false
}

// directives are comments that instruct a linter or scanner rather than explain the code.
var directives = []string{"tflint-ignore", "checkov:skip", "trivy:ignore"}

// comment is one comment with the lines it spans.
type comment struct {
	facts.Comment
	end int
}

// lexComments lists the comments of a file (#, // and /* */), without their delimiters.
func lexComments(p string, src []byte) []comment {
	tokens, _ := hclsyntax.LexConfig(src, p, hcl.InitialPos)
	var out []comment
	for _, t := range tokens {
		if t.Type != hclsyntax.TokenComment {
			continue
		}
		raw := strings.TrimRight(string(t.Bytes), "\r\n")
		text := raw
		for _, d := range []string{"#", "//", "/*"} {
			if strings.HasPrefix(text, d) {
				text = text[len(d):]
				break
			}
		}
		text = strings.TrimSpace(strings.TrimSuffix(text, "*/"))
		directive := false
		for _, d := range directives {
			directive = directive || strings.HasPrefix(text, d)
		}
		out = append(out, comment{Comment: facts.Comment{Path: p, Line: t.Range.Start.Line, Text: text, Directive: directive},
			end: t.Range.Start.Line + strings.Count(raw, "\n")})
	}
	return out
}
