//go:build cgo

package treesitter

import (
	"sync"

	sitter "github.com/tree-sitter/go-tree-sitter"

	"github.com/rerorets/hadome/internal/adapter/reuse"
)

// parsed is one file read: the copy of the tree read from src, and the nodes a repairer took out of
// earlier copies. It holds only Go memory: every C tree is closed once copied. Base and head share
// it when the file is the same in both, so nothing changes it after it is made; what a version
// learns about the file goes in that version's file.
type parsed struct {
	src    []byte
	root   *node
	lifted []*node
}

// file is the parsed file as one version reads it.
func (pr *parsed) file(p string) *file {
	return &file{path: p, src: pr.src, root: pr.root, lifted: pr.lifted}
}

// parseInput is everything a file's trees depend on: the language, whose grammar reads it; the
// path, which picks the grammar (TypeScript's .tsx); and the content.
type parseInput struct {
	Lang string
	Path string
	Src  []byte
}

var parseStage = reuse.NewPure("treesitter/parse", reuse.ScopeLanguage, parseFile)

// grammars are the languages' readers, for their grammars and repairs, which keep no state.
var grammars = func() map[string]reader {
	out := map[string]reader{}
	for _, l := range Languages() {
		out[l.name] = l.r
	}
	return out
}()

var parsers = sync.Pool{New: func() any { return sitter.NewParser() }}

func parseFile(in parseInput) (*parsed, error) {
	parser := parsers.Get().(*sitter.Parser)
	defer parsers.Put(parser)
	return parseWith(grammars[in.Lang], parser, in.Path, in.Src)
}

// parseWith parses src with the grammar r gives the path, reading it again while a repairer
// finds parts the grammar misread. Each tree is copied and closed at once, so no C tree outlives it.
func parseWith(r reader, parser *sitter.Parser, p string, src []byte) (*parsed, error) {
	g := r.grammar(p)
	if err := parser.SetLanguage(g); err != nil {
		return nil, err
	}
	root := parseCopy(parser, g, src)
	pr := &parsed{src: src}
	if rp, ok := r.(repairer); ok {
		read := src
		for range 4 { // a repair may uncover another misreading further on
			fixed, lifted := rp.repair(read, root)
			if fixed == nil {
				break
			}
			pr.lifted = append(pr.lifted, lifted...)
			read = fixed
			root = parseCopy(parser, g, read)
		}
	}
	pr.root = root
	return pr, nil
}

// parseCopy parses src with the parser, set to grammar g, and returns the copy of the tree.
func parseCopy(parser *sitter.Parser, g *sitter.Language, src []byte) *node {
	tree := parser.Parse(src, nil)
	defer tree.Close()
	return copyTree(tree, g)
}
