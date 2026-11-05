// Package notices writes and checks THIRD_PARTY_NOTICES, the file that carries the licenses of every
// module linked into hadome next to the binary in every release archive and package. It knows
// nothing of how the module list is found: the caller lists the modules and reads their license files.
//
// Design: docs/design/distribution.md
package notices

import (
	"bytes"
	"fmt"
	"sort"
	"strings"
	"unicode"
)

// File is the name of the notices file at the repository's root.
const File = "THIRD_PARTY_NOTICES"

// Module is one module linked into the binary, with the license files it ships.
type Module struct {
	// Path is the module path; the Go standard library has a name instead, and no Version.
	Path    string
	Version string
	// Source is where the module's source can be downloaded.
	Source   string
	Licenses []License
}

// License is one license or notice file of a module.
type License struct {
	Name string // the file's name, such as LICENSE or NOTICE
	Text string
}

// ProxySource is where the Go module proxy serves a module's source. An upper-case letter in the
// path or version is written as '!' and its lower case, as the proxy names it.
func ProxySource(path, version string) string {
	return fmt.Sprintf("https://proxy.golang.org/%s/@v/%s.zip", escape(path), escape(version))
}

func escape(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsUpper(r) {
			b.WriteByte('!')
			r = unicode.ToLower(r)
		}
		b.WriteRune(r)
	}
	return b.String()
}

const rule = "================================================================================\n"

const header = "Third-party software in hadome\n\n" +
	"The hadome executable is built from the Go standard library and the modules below. Each section\n" +
	"gives a module, its version, where its source code can be downloaded, and its license as the module\n" +
	"ships it. hadome itself is under the MIT license in LICENSE.\n\n" +
	"This file is generated: go test ./cmd/hadome -run TestThirdPartyNoticesListEveryModule -update-notices\n"

// title is the first line of a module's section.
func (m Module) title() string { return strings.TrimSpace(m.Path + " " + m.Version) }

func (m Module) section() string {
	var b strings.Builder
	b.WriteString(m.title() + "\nSource: " + m.Source + "\n" + rule)
	for _, l := range m.Licenses {
		text := strings.TrimRight(strings.ReplaceAll(l.Text, "\r\n", "\n"), "\n")
		b.WriteString("\n--- " + l.Name + "\n\n" + text + "\n")
	}
	return b.String()
}

// Render is the notices file for these modules: the first is the standard library, the rest sorted
// by path.
func Render(stdlib Module, mods []Module) []byte {
	mods = sorted(mods)
	var b bytes.Buffer
	b.WriteString(header)
	for _, m := range append([]Module{stdlib}, mods...) {
		b.WriteString("\n" + rule + m.section())
	}
	return b.Bytes()
}

func sorted(mods []Module) []Module {
	out := append([]Module(nil), mods...)
	sort.Slice(out, func(i, j int) bool { return out[i].Path < out[j].Path })
	return out
}

// Problems lists what keeps a notices file from shipping with a binary built from these modules: a
// module with no license file, a module or version with no section, a section whose source or license
// text is out of date, and a section for a module or version no longer linked in. It is empty when the file is
// exactly what Render writes.
func Problems(file []byte, stdlib Module, mods []Module) []string {
	var out []string
	for _, m := range mods {
		if len(m.Licenses) == 0 {
			out = append(out, m.title()+": ships no license file; find its license before shipping it")
		}
	}
	have := sections(file)
	for _, m := range append([]Module{stdlib}, sorted(mods)...) {
		got, ok := have[m.title()]
		delete(have, m.title())
		switch {
		case !ok:
			out = append(out, m.title()+": missing")
		case got != m.section():
			out = append(out, m.title()+": the source or license text is out of date")
		}
	}
	var extra []string
	for t := range have {
		extra = append(extra, t+": no longer linked into hadome")
	}
	sort.Strings(extra)
	out = append(out, extra...)
	if len(out) == 0 && !bytes.Equal(normalise(file), Render(stdlib, mods)) {
		out = append(out, "the header or the order of the sections differs from what is generated")
	}
	return out
}

func normalise(b []byte) []byte { return bytes.ReplaceAll(b, []byte("\r\n"), []byte("\n")) }

// sections splits a notices file into its sections by title; each holds what section() writes.
func sections(file []byte) map[string]string {
	out := map[string]string{}
	parts := strings.Split(string(normalise(file)), "\n"+rule)
	for i := 1; i+1 < len(parts); i += 2 {
		title, _, _ := strings.Cut(parts[i], "\n")
		out[title] = parts[i] + "\n" + rule + parts[i+1]
	}
	return out
}
