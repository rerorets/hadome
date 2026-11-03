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

// viewpoint: state integrity
// target: hadome init
// Suggest invariant 3: init never overwrites a file, and what it writes is valid configuration.
func TestInitWritesValidConfigurationAndNeverOverwrites(t *testing.T) {
	t.Chdir(writeTree(t, map[string]string{"main.go": "package main\n", "web/app.ts": "", "node_modules/x/y.py": ""}))
	code, out := runIn(t, "init")
	if code != 0 || !strings.Contains(out, "detected: go, typescript") {
		t.Fatalf("exit %d: %s", code, out)
	}
	if code, out := runIn(t, "config", "check"); code != 0 {
		t.Fatalf("init wrote an invalid file: %s", out)
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
