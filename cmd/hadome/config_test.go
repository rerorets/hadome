package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

// viewpoint: external contract
// target: hadome config check, hadome explain (command line)
// Agents rely on these exit codes to know whether their configuration change is acceptable.
func TestConfigCheckAndExplainExitCodes(t *testing.T) {
	cases := []struct {
		name string
		yml  string // empty means no configuration file
		args []string
		want int
		out  string // must appear in stdout or stderr
	}{
		{"no file", "", []string{"config", "check"}, 0, "built-in settings"},
		{"valid file", "schema: 1\nlanguages: [go]\n", []string{"config", "check"}, 0, "is valid"},
		{"invalid file", "schema: 1\nlanguage: [go]\n", []string{"config", "check"}, 2, ".hadome.yml:2: language"},
		{"known value", "", []string{"explain", "round-trip"}, 0, "Writing out and reading back"},
		{"a rule", "", []string{"explain", "tests/no-sleep"}, 0, "Self-test cases:\n  - a new sleep fails"},
		{"unknown name", "", []string{"explain", "nonsense"}, 2, "nothing called"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			files := map[string]string{}
			if tc.yml != "" {
				files[".hadome.yml"] = tc.yml
			}
			t.Chdir(writeTree(t, files))
			var out, errOut bytes.Buffer
			if got := run(tc.args, &out, &errOut); got != tc.want {
				t.Errorf("exit %d, want %d; stderr: %s", got, tc.want, errOut.String())
			}
			if !strings.Contains(out.String()+errOut.String(), tc.out) {
				t.Errorf("output does not mention %q:\n%s%s", tc.out, out.String(), errOut.String())
			}
		})
	}
}

// viewpoint: invariant
// target: hadome explain <rule>
// Every built-in rule explains itself: what it finds, why, how to fix it, and keys that exist.
func TestEveryBuiltInRuleExplainsItself(t *testing.T) {
	for _, r := range newEngine().Rules {
		d, ok := r.(engine.Describer)
		if !ok {
			t.Errorf("%s does not explain itself", r.ID())
			continue
		}
		a := d.About()
		if a.Finds == "" || a.Why == "" || a.Fix == "" {
			t.Errorf("%s: Finds, Why and Fix must all be written: %+v", r.ID(), a)
		}
		for _, k := range a.Keys {
			if _, ok := config.Explain(k); !ok {
				t.Errorf("%s names %s, which is not a configuration key", r.ID(), k)
			}
		}
		if _, ok := explainRule(r.ID()); !ok {
			t.Errorf("hadome explain %s finds nothing", r.ID())
		}
	}
}
