package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/config"
)

func runIn(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := run(args, &out, &errOut)
	return code, out.String() + errOut.String()
}

// viewpoint: external contract
// target: hadome help, --help, -h
// Asking for help is not a mistake: it prints the usage to standard output and exits 0, and the
// usage names every flag a person needs to compare, including the two directories.
func TestHelpPrintsTheUsage(t *testing.T) {
	for _, arg := range []string{"help", "--help", "-h"} {
		var out, errOut bytes.Buffer
		if code := run([]string{arg}, &out, &errOut); code != 0 || errOut.Len() != 0 {
			t.Errorf("%s: exit %d, stderr %q", arg, code, errOut.String())
		}
		for _, want := range []string{"hadome check", "hadome mutate", "--base-dir", "--head-dir"} {
			if !strings.Contains(out.String(), want) {
				t.Errorf("%s: the usage does not mention %s:\n%s", arg, want, out.String())
			}
		}
	}
}

// viewpoint: external contract
// target: hadome agents
// People copy the block from the README; agents get it from hadome. Both must say the same.
func TestTheAgentBlockIsTheREADMEs(t *testing.T) {
	for _, readme := range []string{"README.md", "README.ja.md"} {
		data, err := os.ReadFile(filepath.Join("..", "..", readme))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), agentBlock) {
			t.Errorf("%s does not contain the block `hadome agents` prints", readme)
		}
	}
}

// viewpoint: external contract
// target: hadome agents
// This repository's own instructions for agents carry the block hadome prints, in each language:
// a block edited by hand in one of them tells agents something hadome no longer says.
func TestTheAgentBlockIsThisRepositorysInstructions(t *testing.T) {
	for file, block := range map[string]string{"CLAUDE.md": agentBlock, "CLAUDE.ja.md": agentBlockJa} {
		data, err := os.ReadFile(filepath.Join("..", "..", file))
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(data), block) {
			t.Errorf("%s does not contain the block `hadome agents` prints in its language", file)
		}
	}
}

// viewpoint: state integrity
// target: hadome agents --write
// Suggest invariant 2: writing the block twice leaves the file as the first write did, and
// whatever else the file says is kept.
func TestWritingTheAgentBlockIsIdempotent(t *testing.T) {
	stale := beginMarker + "\nold text\n" + endMarker + "\n"
	cases := map[string]string{
		"missing file":      "",
		"file without one":  "# Notes\n\nKeep tests fast.\n",
		"file with a stale": "# Notes\n\n" + stale + "\nAfter.\n",
	}
	for name, before := range cases {
		t.Run(name, func(t *testing.T) {
			files := map[string]string{}
			if before != "" {
				files["AGENTS.md"] = before
			}
			t.Chdir(writeTree(t, files))
			var first string
			for i := range 2 {
				if code, out := runIn(t, "agents", "--write", "AGENTS.md"); code != 0 {
					t.Fatalf("exit %d: %s", code, out)
				}
				data, _ := os.ReadFile("AGENTS.md")
				if i == 0 {
					first = string(data)
				} else if string(data) != first {
					t.Fatalf("the second write changed the file:\n%s\nthen\n%s", first, data)
				}
			}
			if strings.Count(first, beginMarker) != 1 || !strings.Contains(first, agentBlock) || strings.Contains(first, "old text") {
				t.Errorf("want exactly one current block:\n%s", first)
			}
			for _, kept := range []string{"Keep tests fast.", "After."} {
				if strings.Contains(before, kept) && !strings.Contains(first, kept) {
					t.Errorf("lost %q:\n%s", kept, first)
				}
			}
		})
	}
}

func TestAHalfBlockIsNotGuessedAt(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"AGENTS.md": beginMarker + "\nno end\n"}))
	if code, out := runIn(t, "agents", "--write", "AGENTS.md"); code != 2 || !strings.Contains(out, "by hand") {
		t.Fatalf("exit %d: %s", code, out)
	}
}

// viewpoint: behaviour
// target: hadome agents --write
// Every file listed gets the block, whether named by a relative or an absolute path.
func TestTheAgentBlockGoesIntoEveryFileListed(t *testing.T) {
	dir := writeTree(t, map[string]string{"CLAUDE.md": "# Notes\n", ".github/keep": ""})
	t.Chdir(dir)
	abs := filepath.Join(dir, ".github", "copilot-instructions.md")
	if code, out := runIn(t, "agents", "--write", "AGENTS.md", "CLAUDE.md", abs); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	for _, p := range []string{"AGENTS.md", "CLAUDE.md", abs} {
		if data, _ := os.ReadFile(p); !strings.Contains(string(data), agentBlock) {
			t.Errorf("%s has no block:\n%s", p, data)
		}
	}
}

// viewpoint: state integrity
// target: hadome agents --write
// Suggest invariant 3: when one file listed cannot take the block, no file changes, and every
// problem is reported.
func TestAFailedWriteChangesNoFile(t *testing.T) {
	half := beginMarker + "\nno end\n"
	t.Chdir(writeTree(t, map[string]string{"CLAUDE.md": "# Notes\n", "HALF.md": half}))
	code, out := runIn(t, "agents", "--write", "CLAUDE.md", "AGENTS.md", ".githbu/copilot.md", "HALF.md")
	if code != 2 || !strings.Contains(out, ".githbu") || !strings.Contains(out, "by hand") {
		t.Fatalf("want exit 2 naming both problems; got exit %d: %s", code, out)
	}
	if data, _ := os.ReadFile("CLAUDE.md"); string(data) != "# Notes\n" {
		t.Errorf("CLAUDE.md changed:\n%s", data)
	}
	if _, err := os.Stat("AGENTS.md"); err == nil {
		t.Error("AGENTS.md was created")
	}
	if _, err := os.Stat(".githbu"); err == nil {
		t.Error("the mistyped directory was created")
	}
}

// viewpoint: state integrity
// target: hadome init
// Suggest invariant 3: init never overwrites a file, and what it writes is valid configuration.
func TestInitWritesValidConfigurationAndNeverOverwrites(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"go.mod": "module example.com/app\n", "main.go": "package main\n",
		"package.json": "{}\n", "web/app.ts": "", "node_modules/x/y.py": ""}))
	code, out := runIn(t, "init")
	if code != 0 || !strings.Contains(out, "detected: go, typescript") {
		t.Fatalf("exit %d: %s", code, out)
	}
	if code, out := runIn(t, "config", "check"); code != 0 {
		t.Fatalf("init wrote an invalid file: %s", out)
	}
	written, err := os.ReadFile(config.FileName)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(written), "#   test: [go, test, ./...]  # go\n#   # test: [npm, test]  # typescript\n") {
		t.Fatalf("init did not offer a commented test command per language:\n%s", written)
	}
	// Uncommenting every line of the setting once, as an editor's toggle does, is valid
	// configuration: the other language's command stays a comment instead of a second test key.
	lines := strings.Split(string(written), "\n")
	for i, l := range lines {
		if l == "# mutation:" {
			for j := i; j < len(lines) && strings.HasPrefix(lines[j], "# "); j++ {
				lines[j] = strings.TrimPrefix(lines[j], "# ")
			}
		}
	}
	uncommented := strings.Join(lines, "\n")
	if err := os.WriteFile(config.FileName, []byte(uncommented), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, out := runIn(t, "config", "check"); code != 0 || !strings.Contains(uncommented, "\nmutation:\n  test: [go, test, ./...]") {
		t.Fatalf("the uncommented mutation setting is invalid: %s\n%s", out, uncommented)
	}
	if err := os.WriteFile(config.FileName, []byte("schema: 1\n# mine\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if code, _ := runIn(t, "init"); code != 2 {
		t.Fatalf("init over an existing file must fail, got %d", code)
	}
	if data, _ := os.ReadFile(config.FileName); string(data) != "schema: 1\n# mine\n" {
		t.Fatalf("init overwrote the file: %s", data)
	}
}

// viewpoint: behaviour
// target: hadome init
// A test command is offered only where it can run: npm test without a package.json at the root
// fails before any mutant is tried. A language without its manifest gets a note instead.
func TestInitOffersATestCommandOnlyWithItsManifest(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"go.mod": "module example.com/app\n", "main.go": "package main\n", "web/app.ts": ""}))
	if code, out := runIn(t, "init"); code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	written, err := os.ReadFile(config.FileName)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(written), "npm") {
		t.Errorf("npm test offered without a package.json:\n%s", written)
	}
	if !strings.Contains(string(written), "#   test: [go, test, ./...]  # go\n") ||
		!strings.Contains(string(written), "No test command for typescript: no package.json at the root") {
		t.Errorf("want go's command and a note for typescript:\n%s", written)
	}
}

// viewpoint: behaviour
// target: hadome init
// Ansible's YAML has no extension of its own, so init finds it by the layout the Ansible adapter
// claims, and not in YAML outside that layout.
func TestInitDetectsAnsibleByLayout(t *testing.T) {
	for _, tc := range []struct {
		file, want string
	}{
		{"roles/web/tasks/main.yml", "detected: ansible"},
		{"playbooks/web.yml", "detected: ansible"},
		{"site.yml", "detected: ansible"},
		{"ci/build.yml", "detected: none yet"},
	} {
		t.Run(tc.file, func(t *testing.T) {
			t.Chdir(writeTree(t, map[string]string{tc.file: "- hosts: all\n"}))
			code, out := runIn(t, "init")
			if code != 0 || !strings.Contains(out, tc.want) {
				t.Errorf("exit %d, want %q: %s", code, tc.want, out)
			}
		})
	}
}

// viewpoint: external contract
// target: hadome suggest
// Accepting a candidate is pasting it: the output must be valid configuration as it stands.
func TestSuggestPrintsConfigurationToPaste(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{
		"go.mod":     "module example.com/app\n",
		"api/api.go": "package api\n\nfunc CreatePost() {}\n\nfunc ExportRows() {}\n\nfunc ImportRows() {}\n",
	}))
	code, out := runIn(t, "suggest")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, out)
	}
	if err := os.WriteFile(config.FileName, []byte("schema: 1\n"+out), 0o644); err != nil {
		t.Fatal(err)
	}
	c, err := config.Load(os.DirFS("."), ".")
	if err != nil {
		t.Fatalf("suggest's output is not valid configuration: %v\n%s", err, out)
	}
	if len(c.Invariants) != 2 {
		t.Fatalf("want both candidates, got %+v", c.Invariants)
	}
	if code, out := runIn(t, "suggest"); code != 0 || !strings.Contains(out, "No invariant candidates") {
		t.Fatalf("once pasted, nothing is left to suggest: %s", out)
	}
}
