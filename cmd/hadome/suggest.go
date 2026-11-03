package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"sort"
	"strings"

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
	".kt": "kotlin", ".kts": "kotlin",
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
`

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
	langs, err := detectLanguages(os.DirFS("."))
	if err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	detected := strings.Join(langs, ", ")
	if detected == "" {
		detected = "none yet"
	}
	text := fmt.Sprintf(initTemplate, detected, strings.Join(langs, ", "))
	if err := os.WriteFile(config.FileName, []byte(text), 0o644); err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	fmt.Fprintf(stdout, "ok: wrote %s (detected: %s). Next: `hadome suggest` lists invariant candidates.\n", config.FileName, detected)
	return engine.ExitOK
}

func detectLanguages(tree fs.FS) ([]string, error) {
	found := map[string]bool{}
	err := fs.WalkDir(tree, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		name := d.Name()
		if d.IsDir() && p != "." && (name == "vendor" || name == "node_modules" || strings.HasPrefix(name, ".")) {
			return fs.SkipDir
		}
		if lang, ok := extensions[path.Ext(name)]; ok && !d.IsDir() {
			found[lang] = true
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
