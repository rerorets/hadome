package main

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"unicode"
)

var updateNotices = flag.Bool("update-notices", false, "rewrite THIRD_PARTY_NOTICES from the module cache")

// noticesFile is shipped next to the binary in every release archive and package.
const noticesFile = "THIRD_PARTY_NOTICES"

// noticesPlatforms are the systems hadome is released for. The set of modules linked in can differ
// between them, and the release builds with cgo, so every one is listed with cgo on.
var noticesPlatforms = []string{"linux", "darwin", "windows"}

type linkedModule struct{ path, version, dir string }

// linkedModules returns the modules other than hadome itself that the released binary is built from.
func linkedModules(t *testing.T, root string) []linkedModule {
	t.Helper()
	seen := map[string]linkedModule{}
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
			if len(f) != 3 {
				continue
			}
			seen[f[0]+"@"+f[1]] = linkedModule{f[0], f[1], f[2]}
		}
	}
	var mods []linkedModule
	for _, m := range seen {
		mods = append(mods, m)
	}
	slices.SortFunc(mods, func(a, b linkedModule) int { return strings.Compare(a.path, b.path) })
	return mods
}

// licenseFiles returns the license and notice files at the top of dir. A module without one cannot be
// shipped until someone finds out under what terms it may be.
func licenseFiles(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var files []string
	for _, e := range entries {
		upper := strings.ToUpper(e.Name())
		if e.Type().IsRegular() && (strings.HasPrefix(upper, "LICENSE") || strings.HasPrefix(upper, "LICENCE") ||
			strings.HasPrefix(upper, "COPYING") || strings.HasPrefix(upper, "NOTICE")) {
			files = append(files, filepath.Join(dir, e.Name()))
		}
	}
	return files
}

// proxyPath escapes a module path the way the Go module proxy names it: an upper-case letter becomes
// '!' and its lower case.
func proxyPath(p string) string {
	var b strings.Builder
	for _, r := range p {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

const noticesRule = "================================================================================\n"

// notices is the content THIRD_PARTY_NOTICES must have for the modules the binary is built from.
func notices(t *testing.T, root string) []byte {
	t.Helper()
	var b bytes.Buffer
	b.WriteString("Third-party software in hadome\n\n" +
		"The hadome executable is built from the Go standard library and the modules below. Each section\n" +
		"gives a module, its version, where its source code can be downloaded, and its license as the module\n" +
		"ships it. hadome itself is under the MIT license in LICENSE.\n\n" +
		"This file is generated: go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices\n")
	section := func(title, source string, files []string) {
		b.WriteString("\n" + noticesRule + title + "\nSource: " + source + "\n" + noticesRule)
		for _, f := range files {
			text, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			text = bytes.ReplaceAll(text, []byte("\r\n"), []byte("\n"))
			b.WriteString("\n--- " + filepath.Base(f) + "\n\n")
			b.Write(bytes.TrimRight(text, "\n"))
			b.WriteString("\n")
		}
	}
	// Not every Go installation keeps its LICENSE in GOROOT, so the standard library's is a copy.
	section("Go standard library", "https://go.dev/dl/", []string{filepath.Join("testdata", "go", "LICENSE")})
	for _, m := range linkedModules(t, root) {
		files := licenseFiles(t, m.dir)
		if len(files) == 0 {
			t.Errorf("%s %s has no license file at the top of %s; find its license before shipping it", m.path, m.version, m.dir)
		}
		section(m.path+" "+m.version,
			fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.zip", proxyPath(m.path), proxyPath(m.version)), files)
	}
	return b.Bytes()
}

// viewpoint: external contract
// target: THIRD_PARTY_NOTICES
// The licenses of the modules linked into hadome (MIT, BSD, Apache-2.0, MPL-2.0) all require their
// text to travel with the binary, and MPL-2.0 also where the source is. A dependency added or upgraded
// without regenerating the file would ship without its notice.
func TestThirdPartyNoticesListEveryModule(t *testing.T) {
	root := filepath.Join("..", "..")
	want := notices(t, root)
	path := filepath.Join(root, noticesFile)
	if *updateNotices {
		if err := os.WriteFile(path, want, 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	got = bytes.ReplaceAll(got, []byte("\r\n"), []byte("\n"))
	if !bytes.Equal(got, want) {
		t.Errorf("%s does not match the modules hadome is built from and their licenses; regenerate it:\n"+
			"  go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices", noticesFile)
	}
}
