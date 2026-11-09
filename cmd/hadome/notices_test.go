package main

import (
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/notices"
)

var updateNotices = flag.Bool("update-notices", false, "rewrite THIRD_PARTY_NOTICES from the module cache")

// noticesPlatforms are the systems hadome is released for. The set of modules linked in can differ
// between them, and the release builds with cgo, so every one is listed with cgo on.
var noticesPlatforms = []string{"linux", "darwin", "windows"}

// linkedModules returns the modules other than hadome itself that the released binary is built from,
// with the license files at the top of each module's directory in the module cache.
func linkedModules(t *testing.T, root string) []notices.Module {
	t.Helper()
	seen := map[string]bool{}
	var mods []notices.Module
	for _, goos := range noticesPlatforms {
		cmd := exec.Command("go", "list", "-deps", "-f",
			"{{with .Module}}{{if not .Main}}{{.Path}}\t{{.Version}}\t{{.Dir}}{{end}}{{end}}", "./cmd/hadome")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "CGO_ENABLED=1", "GOOS="+goos, "GOARCH=amd64")
		out, err := cmd.Output()
		if err != nil {
			t.Fatalf("go list for %s: %v", goos, err)
		}
		for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 3 || seen[f[0]+"@"+f[1]] {
				continue
			}
			seen[f[0]+"@"+f[1]] = true
			mods = append(mods, notices.Module{Path: f[0], Version: f[1], Source: notices.ProxySource(f[0], f[1]),
				Licenses: licenseFiles(t, f[2])})
		}
	}
	return append(mods, grammarModules(t, root)...)
}

// grammarSources are where the SQL grammars of internal/adapter/sql/grammar come from. Their parsers
// are generated into hadome's own module, so no module of the cache carries their licenses.
var grammarSources = map[string]string{
	"googlesql":  "https://github.com/bytebase/parser/tree/main/googlesql",
	"mysql":      "https://github.com/antlr/grammars-v4/tree/master/sql/mysql/Positive-Technologies",
	"plsql":      "https://github.com/antlr/grammars-v4/tree/master/sql/plsql",
	"postgresql": "https://github.com/antlr/grammars-v4/tree/master/sql/postgresql",
	"snowflake":  "https://github.com/antlr/grammars-v4/tree/master/sql/snowflake",
	"sqlite":     "https://github.com/antlr/grammars-v4/tree/master/sql/sqlite",
	"tsql":       "https://github.com/bytebase/parser/tree/main/tsql",
}

// grammarModules returns a section for each SQL grammar, with the license files of its directory.
func grammarModules(t *testing.T, root string) []notices.Module {
	t.Helper()
	base := filepath.Join(root, "internal", "adapter", "sql", "grammar")
	entries, err := os.ReadDir(base)
	if err != nil {
		t.Fatal(err)
	}
	var out []notices.Module
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		src, ok := grammarSources[e.Name()]
		if !ok {
			t.Fatalf("grammar %s has no source in grammarSources", e.Name())
		}
		out = append(out, notices.Module{Path: "SQL grammar " + e.Name(), Source: src,
			Licenses: licenseFiles(t, filepath.Join(base, e.Name()))})
	}
	return out
}

// licenseFiles reads the license and notice files at the top of dir.
func licenseFiles(t *testing.T, dir string) []notices.License {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []notices.License
	for _, e := range entries {
		upper := strings.ToUpper(e.Name())
		if !e.Type().IsRegular() || !(strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "LICENCE") ||
			strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")) {
			continue
		}
		text, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, notices.License{Name: e.Name(), Text: string(text)})
	}
	return out
}

// stdlib is the Go standard library's section. Not every Go installation keeps its LICENSE in GOROOT,
// so the license is a copy.
func stdlib(t *testing.T) notices.Module {
	t.Helper()
	text, err := os.ReadFile(filepath.Join("testdata", "go", "LICENSE"))
	if err != nil {
		t.Fatal(err)
	}
	return notices.Module{Path: "Go standard library", Source: "https://go.dev/dl/",
		Licenses: []notices.License{{Name: "LICENSE", Text: string(text)}}}
}

// viewpoint: external contract
// target: THIRD_PARTY_NOTICES
// The licenses of the modules linked into hadome (MIT, BSD, Apache-2.0, MPL-2.0) all require their
// text to travel with the binary, and MPL-2.0 also where the source is. A dependency added or upgraded
// without regenerating the file would ship without its notice.
func TestThirdPartyNoticesListEveryModule(t *testing.T) {
	root := filepath.Join("..", "..")
	mods, std := linkedModules(t, root), stdlib(t)
	path := filepath.Join(root, notices.File)
	if *updateNotices {
		if err := os.WriteFile(path, notices.Render(std, mods), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if problems := notices.Problems(got, std, mods); len(problems) > 0 {
		t.Errorf("%s does not match the modules hadome is built from:\n  %s\nregenerate it with\n"+
			"  go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices",
			notices.File, strings.Join(problems, "\n  "))
	}
}
