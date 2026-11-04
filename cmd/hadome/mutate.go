package main

import (
	"flag"
	"fmt"
	"io"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/mutation"
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
	if err := fl.Parse(args); err != nil {
		return engine.ExitConfig
	}
	src, ok := pickSource(*base, *head, *baseDir, *headDir, stderr)
	if !ok {
		return engine.ExitConfig
	}
	an := adapter.New()
	m := &mutation.Mutate{Analyzer: an, Mutator: an, Workdir: ".", Progress: stderr}
	r, err := m.Run(src)
	if err != nil {
		fmt.Fprintln(stderr, "hadome: mutate could not run, so it does not pass:", err)
		return engine.ExitConfig
	}
	if *format == "json" {
		if err := engine.WriteJSON(stdout, r); err != nil {
			return engine.ExitConfig
		}
	} else {
		engine.WriteText(stdout, r)
	}
	return r.ExitCode()
}
