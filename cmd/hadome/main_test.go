package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeTree(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func project(test string) map[string]string {
	return map[string]string{
		"go.mod":         "module example.com/app\n",
		"store/store.go": "package store\n\n// Append adds a row.\nfunc Append(row string) {}\n",
		"store/store_test.go": "package store_test\n\nimport (\n\t\"testing\"\n\t\"time\"\n\n\t\"example.com/app/store\"\n)\n\nvar _ = time.Second\n\n" +
			"func TestAppend(t *testing.T) {\n\tstore.Append(\"a\")\n" + test + "}\n",
	}
}

// viewpoint: external contract
// target: hadome check (command line)
// End to end: the exit code and the JSON shape are what agents and CI depend on.
func TestCheckExitCodesAndJSON(t *testing.T) {
	base := writeTree(t, project(""))
	cases := []struct {
		name string
		head string
		exit int
	}{
		{"no change passes", "", 0},
		{"a regression fails", "\t_ = time.Now()\n", 1},
		{"a new exception needs review", "\t// gate:allow tests/no-clock reason: measures wall time\n\t_ = time.Now()\n", 3},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			head := writeTree(t, project(c.head))
			var out, errOut bytes.Buffer
			code := run([]string{"check", "--base-dir", base, "--head-dir", head, "--format", "json"}, &out, &errOut)
			if code != c.exit {
				t.Fatalf("want exit %d, got %d\n%s%s", c.exit, code, out.String(), errOut.String())
			}
			var report struct {
				Schema      int               `json:"schema"`
				ExitCode    int               `json:"exit_code"`
				Regressions []json.RawMessage `json:"regressions"`
			}
			if err := json.Unmarshal(out.Bytes(), &report); err != nil {
				t.Fatalf("output must be JSON: %v\n%s", err, out.String())
			}
			if report.Schema != 1 || report.ExitCode != c.exit || report.Regressions == nil {
				t.Fatalf("unexpected report: %s", out.String())
			}
		})
	}
}

// viewpoint: fail-closed
// target: hadome check (command line)
// guards: a check that cannot run never passes
func TestCheckThatCannotRunNeverPasses(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"check", "--base-dir", filepath.Join(t.TempDir(), "missing"), "--head-dir", t.TempDir()}, &out, &errOut)
	if code != 2 {
		t.Fatalf("want exit 2 when the base cannot be read, got %d", code)
	}
}

// viewpoint: invariant
// target: newEngine's rules
// Engine invariant 6: every built-in rule has self-test cases, and they all pass. A check never
// runs them, so this is where a rule without cases is stopped before it ships.
func TestEveryBuiltInRuleHasPassingCases(t *testing.T) {
	for _, f := range newEngine().SelfTest() {
		t.Errorf("%s: %s: %s", f.Rule, f.Name, f.Err)
	}
}

// viewpoint: failure handling
// target: hadome check (command line)
// Revisions and directories are two ways to name the versions; giving both is a mistake, not a choice.
func TestCheckRefusesRevisionsAndDirectoriesTogether(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"check", "--head", "main", "--base-dir", t.TempDir(), "--head-dir", t.TempDir()}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "not both") {
		t.Fatalf("got exit %d: %s", code, errOut.String())
	}
}

// viewpoint: failure handling
// target: hadome mutate (command line)
// mutate never guesses a test command: without mutation.test it stops with exit code 2 and says
// what to add.
func TestMutateWithoutConfigurationSaysWhatToAdd(t *testing.T) {
	base, head := writeTree(t, map[string]string{"a.txt": "a"}), writeTree(t, map[string]string{"a.txt": "a"})
	var out, errOut bytes.Buffer
	code := run([]string{"mutate", "--base-dir", base, "--head-dir", head}, &out, &errOut)
	if code != 2 || !strings.Contains(errOut.String(), "mutation.test") {
		t.Fatalf("got exit %d: %s", code, errOut.String())
	}
}
