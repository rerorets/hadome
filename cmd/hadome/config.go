package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/mutation"
)

const usage = `usage:
  hadome check [--base REV] [--head REV] [--format text|json] [--lang en|ja]
                                                    compare base (default: the base branch) with head
                                                    (default: the working tree); exit 0, 1, 2 or 3
  hadome mutate [--base REV] [--head REV] [--format text|json] [--lang en|ja]
                                                    break the functions the change touched and fail when no
                                                    test notices (needs mutation.test); exit 0, 1, 2 or 3
  hadome self-test                                  run every rule's self-test cases
  hadome config check                               validate .hadome.yml without running any rule
  hadome schema                                     print the JSON Schema of .hadome.yml
  hadome explain [--lang en|ja] <rule, key or value>
                                                    describe one rule, configuration key or value
  hadome suggest                                    print invariant candidates as configuration to paste
  hadome agents [--lang en|ja] [--write FILE...]    print, or write into each FILE, the block that tells agents about hadome
  hadome init                                       write a minimal .hadome.yml (never overwrites)
  hadome version                                    print the version

--lang of check, mutate and explain picks the language of the findings, a rule's description and the
summary; without it, messages in .hadome.yml decides, else en.
`

func configCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 1 || args[0] != "check" {
		fmt.Fprint(stderr, usage)
		return engine.ExitConfig
	}
	c, err := config.Load(os.DirFS("."), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return engine.ExitConfig
	}
	if len(c.Layers) == 0 {
		fmt.Fprintf(stdout, "ok: no %s; the built-in settings apply\n", config.FileName)
		return engine.ExitOK
	}
	fmt.Fprintf(stdout, "ok: %s is valid (layers, earliest first: %s)\n", config.FileName, strings.Join(c.Layers, ", "))
	return engine.ExitOK
}

func explain(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("explain", flag.ContinueOnError)
	fl.SetOutput(stderr)
	lang := fl.String("lang", "", "language of a rule's description: en or ja (default: messages in .hadome.yml, else en)")
	if err := fl.Parse(args); err != nil || (*lang != "" && !validLang(*lang, stderr)) {
		return engine.ExitConfig
	}
	args = fl.Args()
	if len(args) != 1 {
		fmt.Fprint(stderr, usage)
		return engine.ExitConfig
	}
	text, ok := config.Explain(args[0])
	if !ok {
		text, ok = explainRule(args[0], pickLang(*lang, configuredLang()))
	}
	if !ok {
		fmt.Fprintf(stderr, "hadome: nothing called %q; try a rule such as tests/no-sleep, a key such as invariants "+
			"or rules.tests.io, or a value such as idempotent\n", args[0])
		return engine.ExitConfig
	}
	fmt.Fprint(stdout, text)
	return engine.ExitOK
}

// configuredLang is `messages` of the working tree's configuration. hadome explain is how people
// read about a broken configuration, so a configuration that does not load leaves it in English
// instead of stopping it.
func configuredLang() string {
	c, err := config.Load(os.DirFS("."), ".")
	if err != nil {
		return ""
	}
	return c.Messages
}

// explainRule describes a built-in rule in lang: what it finds and why, how to resolve it, what
// tunes it, and the self-test cases that pin its behaviour down.
func explainRule(id, lang string) (string, bool) {
	for _, r := range append(newEngine().Rules, mutation.Rule()) {
		d, ok := r.(engine.Describer)
		if !ok || r.ID() != id {
			continue
		}
		a := d.About()
		var b strings.Builder
		fmt.Fprintf(&b, "%s — a rule\n\nFinds: %s\nWhy: %s\nFix: %s\n", id, a.Finds.In(lang), a.Why.In(lang), a.Fix.In(lang))
		if a.Review {
			b.WriteString("Result: asks for a person's review (exit code 3); it never fails the check by itself\n")
		} else {
			b.WriteString("Result: a regression fails the check (exit code 1)\n")
		}
		if len(a.Keys) > 0 {
			b.WriteString("Tuned by: " + strings.Join(a.Keys, ", ") + " (hadome explain <key>)\n")
		}
		b.WriteString("Self-test cases:\n")
		for _, c := range r.Cases() {
			b.WriteString("  - " + c.Name + "\n")
		}
		return b.String(), true
	}
	return "", false
}
