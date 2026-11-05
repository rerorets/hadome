package terraform

import (
	"path"
	"sort"
	"strings"

	"github.com/hashicorp/hcl/v2"
	"github.com/hashicorp/hcl/v2/hclsyntax"

	"github.com/rerorets/hadome/internal/facts"
)

// mocks are the blocks of a test file that replace a provider, a resource, a data source or a
// module with made-up values.
var mocks = map[string]bool{"mock_provider": true, "override_resource": true, "override_data": true, "override_module": true}

// testFile is a test file as its run blocks see it.
type testFile struct {
	path string
	src  []byte
	// root is the module terraform test runs against: the file's directory, or the one above a
	// tests directory.
	root string
	// shared are the variables and mocks written outside run blocks; they apply to every run.
	shared   []*hclsyntax.Block
	comments []comment
}

// tests reads each run block of a test file as one test.
func tests(p string, src []byte, body *hclsyntax.Body, cs []comment) []facts.Test {
	tf := testFile{path: p, src: src, root: path.Dir(p), comments: cs}
	if path.Base(tf.root) == "tests" {
		tf.root = path.Dir(tf.root)
	}
	for _, b := range body.Blocks {
		if b.Type == "variables" || mocks[b.Type] {
			tf.shared = append(tf.shared, b)
		}
	}
	var out []facts.Test
	for _, b := range body.Blocks {
		if b.Type == "run" && len(b.Labels) == 1 {
			out = append(out, tf.run(b))
		}
	}
	return out
}

func (tf testFile) run(b *hclsyntax.Block) facts.Test {
	t := facts.Test{Name: b.Labels[0], Path: tf.path, Line: b.Range().Start.Line, Body: fingerprint(tf.path, tf.src, b),
		Declared: facts.Declarations(docLines(tf.comments, b.Range().Start.Line))}
	target := tf.target(b)
	calls := map[string]int{}
	for _, inner := range append(append([]*hclsyntax.Block{}, tf.shared...), b.Body.Blocks...) {
		switch {
		case inner.Type == "variables":
			for name := range inner.Body.Attributes {
				calls[symbolID(target, "var", name)]++
			}
		case inner.Type == "assert":
			t.Asserts++
			for _, out := range readOutputs(inner) {
				calls[symbolID(target, "output", out)]++
			}
		case mocks[inner.Type]:
			t.Hazards = append(t.Hazards, facts.Hazard{Category: facts.HazardMock, Path: tf.path, Line: inner.Range().Start.Line,
				Evidence: evidence(tf.src, inner)})
		}
	}
	if _, ok := b.Body.Attributes["expect_failures"]; ok {
		t.Asserts++
	}
	for target, n := range calls {
		t.Calls = append(t.Calls, facts.Call{Target: target, Times: n})
	}
	sort.Slice(t.Calls, func(i, j int) bool { return t.Calls[i].Target < t.Calls[j].Target })
	return t
}

// target is the module a run block tests: the one its module block names, or else the root module.
func (tf testFile) target(b *hclsyntax.Block) string {
	target := tf.root
	for _, inner := range b.Body.Blocks {
		attr, ok := inner.Body.Attributes["source"]
		if inner.Type != "module" || !ok {
			continue
		}
		if s, ok := literal(attr.Expr); ok {
			target = external(s)
			if strings.HasPrefix(s, "./") || strings.HasPrefix(s, "../") {
				target = path.Join(tf.root, s)
			}
		}
	}
	return target
}

// readOutputs returns the outputs an assert block's condition reads, once per reading.
func readOutputs(assert *hclsyntax.Block) []string {
	attr, ok := assert.Body.Attributes["condition"]
	if !ok {
		return nil
	}
	var out []string
	for _, tr := range attr.Expr.Variables() {
		if name, ok := outputName(tr); ok {
			out = append(out, name)
		}
	}
	return out
}

// outputName returns the output an assertion reads: output.<name>.
func outputName(tr hcl.Traversal) (string, bool) {
	if len(tr) < 2 || tr.RootName() != "output" {
		return "", false
	}
	attr, ok := tr[1].(hcl.TraverseAttr)
	return attr.Name, ok
}

// evidence is a mock block's header with its target, whitespace normalised: what stays the same when
// the block moves.
func evidence(src []byte, b *hclsyntax.Block) string {
	text := string(b.DefRange().SliceBytes(src))
	if attr, ok := b.Body.Attributes["target"]; ok {
		text += " target = " + string(attr.Expr.Range().SliceBytes(src))
	}
	return strings.Join(strings.Fields(text), " ")
}

// fingerprint is the run block's tokens from its opening brace to its closing one: its name, comments
// and line breaks are left out.
func fingerprint(p string, src []byte, b *hclsyntax.Block) string {
	start, end := b.OpenBraceRange.Start, b.CloseBraceRange.End
	tokens, _ := hclsyntax.LexConfig(src[start.Byte:end.Byte], p, start)
	var words []string
	for _, t := range tokens {
		switch t.Type {
		case hclsyntax.TokenComment, hclsyntax.TokenNewline, hclsyntax.TokenEOF:
			continue
		}
		words = append(words, string(t.Bytes))
	}
	return facts.Fingerprint(words)
}

// docLines returns the lines of the comments that end on the line right before line, without their
// delimiters: the run block's doc comment. A blank line ends it.
func docLines(cs []comment, line int) []string {
	var lines []string
	want := line - 1
	for i := len(cs) - 1; i >= 0; i-- {
		c := cs[i]
		if c.end > want {
			continue
		}
		if c.end < want {
			break
		}
		lines = append(strings.Split(c.Text, "\n"), lines...)
		want = c.Line - 1
	}
	return lines
}
