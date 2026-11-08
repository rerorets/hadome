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
	"unicode"

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
	var files []parsed
	sources := map[string]map[string]string{} // directory -> provider's local name -> source
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		pf, ok := parse(p, src, c)
		a.Files = append(a.Files, pf.file)
		if !ok {
			continue
		}
		files = append(files, pf)
		if pf.file.Role != facts.RoleTest {
			for local, s := range requiredProviders(pf.body) {
				if sources[pf.dir()] == nil {
					sources[pf.dir()] = map[string]string{}
				}
				sources[pf.dir()][local] = mustLiteral(s)
			}
		}
	}
	for _, pf := range files {
		for _, cm := range pf.comments {
			a.Comments = append(a.Comments, cm.Comment)
		}
		if pf.file.Role == facts.RoleTest {
			a.Tests = append(a.Tests, tests(pf.path, pf.src, pf.body, pf.comments)...)
			continue
		}
		a.Imports = append(a.Imports, imports(pf, c.IO, sources[pf.dir()])...)
		if pf.file.Role == facts.RoleSource {
			a.Symbols = append(a.Symbols, symbols(pf.path, pf.body)...)
		}
	}
	return a, nil
}

// parsed is one file HCL read.
type parsed struct {
	path     string
	src      []byte
	body     *hclsyntax.Body
	file     facts.File
	comments []comment
}

func (pf parsed) dir() string { return path.Dir(pf.path) }

// parse reads one file. It reports false, with the file listed as unread or as other, when there is
// nothing more to read from it.
func parse(p string, src []byte, c config.Config) (parsed, bool) {
	dir := path.Dir(p)
	f := facts.File{Path: p, Language: name, Lines: facts.CountLines(src), Unit: dir, UnitPath: dir}
	if strings.HasSuffix(p, ".tfvars") { // values only: nothing to read
		f.Role, f.Unit, f.UnitPath = facts.RoleOther, "", ""
		return parsed{file: f}, false
	}
	hf, diags := hclsyntax.ParseConfig(src, p, hcl.InitialPos)
	body, ok := hf.Body.(*hclsyntax.Body)
	if diags.HasErrors() || !ok {
		return parsed{file: facts.File{Path: p, Language: name, Role: facts.RoleOther, Lines: f.Lines, Unread: true}}, false
	}
	pf := parsed{path: p, src: src, body: body, comments: lexComments(p, src)}
	switch {
	case strings.HasSuffix(p, ".tftest.hcl"):
		f.Role = facts.RoleTest
	case config.MatchesAny(c.GeneratedFiles, p) || generated(pf.comments):
		f.Role = facts.RoleGenerated
	default:
		f.Role = facts.RoleSource
	}
	pf.file = f
	return pf, true
}

// symbolID names a variable or output the way Terraform refers to it inside the module (var.region,
// output.region), so that a variable and an output of the same name stay apart. The root module's
// need no directory in front.
func symbolID(module, kind, name string) string {
	if module == "." {
		return kind + "." + name
	}
	return module + "." + kind + "." + name
}

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

// imports lists the modules a file calls and the providers it uses. A provider is how a module
// reaches outside, so its import performs input and output; a module's does when configuration lists
// it in rules.tests.io. sources maps the providers the module requires by local name to their source.
func imports(pf parsed, io []string, sources map[string]string) []facts.Import {
	dir := pf.dir()
	var out []facts.Import
	add := func(line int, source string, provider bool) {
		im := facts.Import{Path: pf.path, Line: line, FromUnit: dir, Text: source, IO: provider}
		if strings.HasPrefix(source, "./") || strings.HasPrefix(source, "../") {
			im.ToUnit, im.Internal = path.Join(dir, source), true
		} else {
			im.ToUnit = external(source)
		}
		im.IO = im.IO || slices.ContainsFunc(io, func(e string) bool { return im.ToUnit == e || strings.HasPrefix(im.ToUnit, e+"/") })
		out = append(out, im)
	}
	used := map[string]bool{} // providers this file already imports
	for _, b := range pf.body.Blocks {
		switch b.Type {
		case "module":
			if attr, ok := b.Body.Attributes["source"]; ok {
				if s, ok := literal(attr.Expr); ok {
					add(attr.SrcRange.Start.Line, s, false)
				}
			}
		case "terraform":
			for _, src := range sortedExprs(requiredProviders(b.Body)) {
				used[mustLiteral(src)] = true
				add(src.Range().Start.Line, mustLiteral(src), true)
			}
		}
	}
	for _, u := range providerUses(pf.body, sources) {
		if !used[u.source] {
			used[u.source] = true
			add(u.line, u.source, true)
		}
	}
	return out
}

type providerUse struct {
	line   int
	source string
}

// providerUses lists the provider of every resource and data source, in source order.
func providerUses(body *hclsyntax.Body, sources map[string]string) []providerUse {
	var out []providerUse
	for _, b := range body.Blocks {
		if (b.Type == "resource" || b.Type == "data") && len(b.Labels) > 0 {
			out = append(out, providerUse{b.Range().Start.Line, providerOf(b, sources)})
		}
	}
	return out
}

// providerOf is the source of the provider a resource or data source uses: the local name its
// provider argument names, or else its type's first word (aws for aws_vpc), looked up among the
// module's required providers. Terraform reads a name it does not find there as hashicorp/<name>.
func providerOf(b *hclsyntax.Block, sources map[string]string) string {
	local, _, _ := strings.Cut(b.Labels[0], "_")
	if attr, ok := b.Body.Attributes["provider"]; ok {
		if tr, diags := hcl.AbsTraversalForExpr(attr.Expr); !diags.HasErrors() {
			local = tr.RootName()
		}
	}
	if s, ok := sources[local]; ok {
		return s
	}
	return "hashicorp/" + local
}

// requiredProviders returns the source of every provider a terraform block (or a file's body, for the
// terraform blocks in it) requires, by local name.
func requiredProviders(body *hclsyntax.Body) map[string]hclsyntax.Expression {
	out := map[string]hclsyntax.Expression{}
	for _, rp := range body.Blocks {
		switch rp.Type {
		case "terraform":
			for k, v := range requiredProviders(rp.Body) {
				out[k] = v
			}
		case "required_providers":
			for _, attr := range sortedAttributes(rp.Body) {
				obj, ok := attr.Expr.(*hclsyntax.ObjectConsExpr)
				if !ok { // the old form, a version alone, names no source
					continue
				}
				for _, item := range obj.Items {
					if _, ok := literal(item.ValueExpr); ok && hcl.ExprAsKeyword(item.KeyExpr) == "source" {
						out[attr.Name] = item.ValueExpr
					}
				}
			}
		}
	}
	return out
}

// sortedExprs returns the expressions in source order.
func sortedExprs(m map[string]hclsyntax.Expression) []hclsyntax.Expression {
	var out []hclsyntax.Expression
	for _, e := range m {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Range().Start.Byte < out[j].Range().Start.Byte })
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
func generated(cs []comment) bool {
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
		// Line breaks before the text stay, so that its first line is the comment's first line.
		text = strings.TrimRightFunc(strings.TrimLeft(strings.TrimSuffix(text, "*/"), " \t"), unicode.IsSpace)
		directive := false
		for _, d := range directives {
			directive = directive || strings.HasPrefix(text, d)
		}
		end := t.Range.Start.Line + strings.Count(raw, "\n")
		out = append(out, comment{Comment: facts.Comment{Path: p, Line: t.Range.Start.Line, EndLine: end, Text: text, Directive: directive},
			end: end})
	}
	return out
}
