package main

import (
	"bytes"
	"strings"
	"testing"
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
