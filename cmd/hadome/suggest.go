package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/suggest"
)

// suggestCmd prints the working tree's invariant candidates as configuration to paste.
func suggestCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprint(stderr, usage)
		return engine.ExitConfig
	}
	tree := os.DirFS(".")
	c, err := config.Load(tree, ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return engine.ExitConfig
	}
	a, err := newEngine().Analyzer.Analyze(engine.Version{Label: "working tree", FS: tree}, c)
	if err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	cands := suggest.Candidates(engine.Input{Facts: a, Config: c})
	if len(cands) == 0 {
		fmt.Fprintln(stdout, "No invariant candidates that are not configured already.")
		return engine.ExitOK
	}
	fmt.Fprintf(stdout, "# Invariant candidates. Copy the ones that matter into the invariants list of %s.\ninvariants:\n", config.FileName)
	for _, cd := range cands {
		fmt.Fprintf(stdout, "  # %s:%d — %s\n  - type: %s\n", cd.File, cd.Line, cd.Why, cd.Type)
		if len(cd.Targets) == 1 {
			fmt.Fprintf(stdout, "    target: %s\n", cd.Targets[0])
		} else {
			fmt.Fprintf(stdout, "    targets: [%s]\n", strings.Join(cd.Targets, ", "))
		}
	}
	return engine.ExitOK
}

// extensions maps file extensions to the languages of config.Languages.
var extensions = map[string]string{
	".go": "go", ".ts": "typescript", ".tsx": "typescript", ".js": "typescript", ".jsx": "typescript",
	".mjs": "typescript", ".cjs": "typescript", ".py": "python", ".rs": "rust", ".java": "java",
	".kt": "kotlin", ".kts": "kotlin", ".tf": "terraform",
}

const initTemplate = `# hadome configuration. hadome works without this file; everything except schema is optional.
# ` + "`hadome explain <key>`" + ` describes each key; ` + "`hadome config check`" + ` validates the file.
schema: 1

# Detected: %s. Leave languages out to keep detecting them on every run.
# languages: [%s]

# Promises the product keeps. ` + "`hadome suggest`" + ` lists candidates to copy here.
# invariants:
#   - type: idempotent
#     target: api.CreatePost
%s`

// testCommand is a language's usual test command, and the files at the root of the repository that
// show the command can run there (npm test needs a package.json).
type testCommand struct {
	command   string
	manifests []string
}

// testCommands are the usual test commands of each language, offered by init for hadome mutate. They
// are guesses, so init writes them as comments and never turns one on. A language without functions
// to break (Terraform, Ansible) has none.
var testCommands = map[string]testCommand{
	"go":         {"[go, test, ./...]", []string{"go.mod"}},
	"typescript": {"[npm, test]", []string{"package.json"}},
	"python":     {"[python, -m, pytest]", []string{"pyproject.toml", "setup.py", "setup.cfg", "pytest.ini"}},
	"rust":       {"[cargo, test]", []string{"Cargo.toml"}},
	"java":       {"[./gradlew, test]", []string{"gradlew"}},
	"kotlin":     {"[./gradlew, test]", []string{"gradlew"}},
}

// mutationComment is the commented mutation setting. It offers the test command of each language
// found whose manifest is at the root, and says why the others get none. Only the first command is
// one comment level deep; the others are two, so uncommenting the setting's lines once always gives
// valid configuration with a single test key.
func mutationComment(tree fs.FS, langs []string) string {
	var offered, notes []string
	byCommand := map[string][]string{}
	for _, l := range langs {
		tc, ok := testCommands[l]
		if !ok {
			continue
		}
		if !slices.ContainsFunc(tc.manifests, func(m string) bool { _, err := fs.Stat(tree, m); return err == nil }) {
			notes = append(notes, fmt.Sprintf("# No test command for %s: no %s at the root. Write the command that runs its tests.\n",
				l, strings.Join(tc.manifests, ", ")))
			continue
		}
		if len(byCommand[tc.command]) == 0 {
			offered = append(offered, tc.command)
		}
		byCommand[tc.command] = append(byCommand[tc.command], l)
	}
	if len(offered) == 0 && len(notes) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\n# hadome mutate breaks the functions a change touched and runs your tests once per break.\n")
	b.WriteString("# `hadome explain mutation` describes the keys.\n")
	for _, n := range notes {
		b.WriteString(n)
	}
	if len(offered) == 0 {
		return b.String()
	}
	b.WriteString("# Uncomment the lines from mutation: on to turn it on.")
	if len(offered) > 1 {
		b.WriteString(" To run another language's tests, put its command in place of the first.")
	}
	b.WriteString("\n# mutation:\n")
	for i, c := range offered {
		prefix := "#   "
		if i > 0 {
			prefix = "#   # "
		}
		fmt.Fprintf(&b, "%stest: %s  # %s\n", prefix, c, strings.Join(byCommand[c], ", "))
	}
	return b.String()
}

// initCmd writes a minimal configuration file, and never overwrites one.
func initCmd(args []string, stdout, stderr io.Writer) int {
	if len(args) != 0 {
		fmt.Fprint(stderr, usage)
		return engine.ExitConfig
	}
	if _, err := os.Stat(config.FileName); !errors.Is(err, fs.ErrNotExist) {
		fmt.Fprintf(stderr, "hadome: %s already exists; init never overwrites it\n", config.FileName)
		return engine.ExitConfig
	}
	tree := os.DirFS(".")
	langs, err := detectLanguages(tree)
	if err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	detected := strings.Join(langs, ", ")
	if detected == "" {
		detected = "none yet"
	}
	text := fmt.Sprintf(initTemplate, detected, strings.Join(langs, ", "), mutationComment(tree, langs))
	if err := os.WriteFile(config.FileName, []byte(text), 0o644); err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	fmt.Fprintf(stdout, "ok: wrote %s (detected: %s). Next: `hadome suggest` lists invariant candidates.\n", config.FileName, detected)
	return engine.ExitOK
}

// detectLanguages lists the languages of the tree: by extension, and for a language whose files no
// extension tells apart (Ansible's YAML), by the adapter's own claim on the path.
func detectLanguages(tree fs.FS) ([]string, error) {
	byExtension := map[string]bool{}
	for _, l := range extensions {
		byExtension[l] = true
	}
	var byLayout []adapter.Language
	for _, l := range adapter.New().Languages {
		if !byExtension[l.Name()] {
			byLayout = append(byLayout, l)
		}
	}
	found := map[string]bool{}
	err := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && p != "." && (name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".")) {
			return fs.SkipDir
		}
		if d.IsDir() {
			return nil
		}
		if lang, ok := extensions[path.Ext(name)]; ok {
			found[lang] = true
		}
		for _, l := range byLayout {
			if l.Claims(p) {
				found[l.Name()] = true
			}
		}
		return nil
	})
	var out []string
	for l := range found {
		out = append(out, l)
	}
	sort.Strings(out)
	return out, err
}
