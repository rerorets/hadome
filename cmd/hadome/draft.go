package main

import (
	"flag"
	"fmt"
	"io"
	"os"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/deps"
)

// draftCmd prints a draft of the dependency diagram (kind diagram) or the call diagram (kind calls),
// drawn from the working tree as it is.
func draftCmd(kind string, args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("suggest "+kind, flag.ContinueOnError)
	fl.SetOutput(stderr)
	depth := fl.Int("depth", 2, "how many directory levels from the root a box takes in")
	if err := fl.Parse(args); err != nil || fl.NArg() != 0 {
		return engine.ExitConfig
	}
	if *depth < 1 {
		fmt.Fprintln(stderr, "hadome: --depth must be 1 or more")
		return engine.ExitConfig
	}
	in, ok := analyzeTree(stderr)
	if !ok {
		return engine.ExitConfig
	}
	if kind == "calls" {
		fmt.Fprint(stdout, deps.DraftCalls(in.Facts, *depth))
	} else {
		fmt.Fprint(stdout, deps.DraftDiagram(in.Facts, *depth))
	}
	return engine.ExitOK
}

// analyzeTree reads the working tree with its configuration. It reports what went wrong to stderr.
func analyzeTree(stderr io.Writer) (engine.Input, bool) {
	tree := os.DirFS(".")
	c, err := config.Load(tree, ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return engine.Input{}, false
	}
	a, err := newEngine().Analyzer.Analyze(engine.Version{Label: "working tree", FS: tree}, c)
	if err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.Input{}, false
	}
	return engine.Input{Facts: a, Config: c}, true
}
