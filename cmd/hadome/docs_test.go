package main

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// skippedDirs hold Markdown that is not the repository's documentation: fixtures, build output, tool state.
var skippedDirs = []string{"testdata", "dist", "bin", "node_modules"}

// documents returns the repository's Markdown files, relative to the root, without the Japanese ones.
func documents(t *testing.T, root string) []string {
	t.Helper()
	var docs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || slices.Contains(skippedDirs, d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		name := d.Name()
		// cmd/hadome/agents.en.md and cmd/hadome/agents.ja.md are the two languages of the agent
		// block, already a pair.
		if !strings.HasSuffix(name, ".md") || strings.HasSuffix(name, ".ja.md") || strings.HasSuffix(name, ".en.md") {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		docs = append(docs, rel)
		return err
	})
	if err != nil {
		t.Fatal(err)
	}
	return docs
}

// outline is what a translation must keep: the level of every heading, and every code fence, in order.
func outline(text string) []string {
	var out []string
	inFence := false
	for _, line := range strings.Split(text, "\n") {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			out = append(out, "fence")
			continue
		}
		if inFence || !strings.HasPrefix(line, "#") {
			continue
		}
		level, _, ok := strings.Cut(line, " ")
		if ok && strings.Trim(level, "#") == "" {
			out = append(out, level)
		}
	}
	return out
}

// viewpoint: external contract
// target: documentation
// The Japanese document is the source and the English one its translation. A change that edits
// one and not the other leaves readers of one language with a different design.
func TestEveryDocumentHasBothLanguages(t *testing.T) {
	root := filepath.Join("..", "..")
	for _, doc := range documents(t, root) {
		ja := strings.TrimSuffix(doc, ".md") + ".ja.md"
		en, err := os.ReadFile(filepath.Join(root, doc))
		if err != nil {
			t.Fatal(err)
		}
		jp, err := os.ReadFile(filepath.Join(root, ja))
		if err != nil {
			t.Errorf("%s has no Japanese version %s", doc, ja)
			continue
		}
		if a, b := outline(string(jp)), outline(string(en)); !slices.Equal(a, b) {
			t.Errorf("%s and %s differ in headings or code blocks:\n  ja: %v\n  en: %v", ja, doc, a, b)
		}
	}
}

// viewpoint: external contract
// target: documentation
// A Japanese document without its English one is a design that English readers never see.
func TestEveryJapaneseDocumentHasAnEnglishOne(t *testing.T) {
	root := filepath.Join("..", "..")
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if path != root && (strings.HasPrefix(d.Name(), ".") || slices.Contains(skippedDirs, d.Name())) {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(d.Name(), ".ja.md") || d.Name() == "agents.ja.md" {
			return nil
		}
		if _, err := os.Stat(strings.TrimSuffix(path, ".ja.md") + ".md"); err != nil {
			t.Errorf("%s has no English version", path)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
