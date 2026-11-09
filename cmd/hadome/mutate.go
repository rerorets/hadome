package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/source"
)

// mutate is hadome mutate: like check, it compares base with head and prints the same report, but
// what it compares is which breaks of the touched functions the tests notice.
func mutate(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("mutate", flag.ContinueOnError)
	fl.SetOutput(stderr)
	base := fl.String("base", "", "base revision (default: origin/main, else main)")
	head := fl.String("head", "", "head revision (default: the working tree)")
	format := fl.String("format", "text", "output format: text or json")
	baseDir := fl.String("base-dir", "", "compare two directories instead of git: base directory")
	headDir := fl.String("head-dir", "", "compare two directories instead of git: head directory")
	lang := fl.String("lang", "", "language of the findings' wording and the summary: en or ja (default: messages in .hadome.yml, else en)")
	if err := fl.Parse(args); err != nil || (*lang != "" && !validLang(*lang, stderr)) {
		return engine.ExitConfig
	}
	src, ok := pickSource(*base, *head, *baseDir, *headDir, stderr)
	if !ok {
		return engine.ExitConfig
	}
	root := "." // the repository hadome runs in, or the head directory
	if d, ok := src.(source.Dirs); ok {
		root = d.HeadDir
	}
	trees, err := mutation.TreesFor(root)
	if err != nil {
		fmt.Fprintln(stderr, "hadome: mutate could not run, so it does not pass:", err)
		return engine.ExitConfig
	}
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: ".", Progress: stderr, Lang: *lang, Trees: trees}
	r, err := m.Run(src)
	if err != nil {
		fmt.Fprintln(stderr, "hadome: mutate could not run, so it does not pass:", err)
		return engine.ExitConfig
	}
	return writeReport(stdout, r, *format, pickLang(*lang, r.Messages))
}
