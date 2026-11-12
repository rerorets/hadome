package engine_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"strings"
	"sync"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/size"
	"github.com/rerorets/hadome/internal/rules/tests"
)

// treeSource is two in-memory trees. It is real data, not a stand-in for behaviour.
type treeSource struct {
	base, head fs.FS
	renames    map[string]string
}

func (s treeSource) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: s.base}, nil
}
func (s treeSource) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: s.head}, nil
}
func (s treeSource) Renames() (map[string]string, error) { return s.renames, nil }

func module(test string) fstest.MapFS {
	return fstest.MapFS{
		"go.mod":         {Data: []byte("module example.com/app\n")},
		"store/store.go": {Data: []byte("package store\n\n// Append adds a row.\nfunc Append(row string) error { return nil }\n")},
		"store/store_test.go": {Data: []byte("package store_test\n\nimport (\n\t\"testing\"\n\t\"time\"\n\n\t\"example.com/app/store\"\n)\n\n" +
			"func TestAppend(t *testing.T) {\n\tif err := store.Append(\"a\"); err != nil {\n\t\tt.Fatal(err)\n\t}\n" + test + "}\n")},
	}
}

func check(t *testing.T, src engine.Source) engine.Report {
	t.Helper()
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	r, err := e.Check(src)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// viewpoint: state integrity
// target: engine.Engine.Check
// Unit tests on in-memory trees: the engine is pure apart from the source, which is real data here.
// guards: base-never-blocks
func TestExistingFindingNeverFailsWhereverItMoves(t *testing.T) {
	base := module("\t_ = time.Now()\n")
	head := module("\n\n\t_ = time.Now()\n")
	head["store/rows_test.go"], head["store/store_test.go"] = head["store/store_test.go"], nil
	delete(head, "store/store_test.go")
	r := check(t, treeSource{base: base, head: head, renames: map[string]string{"store/store_test.go": "store/rows_test.go"}})
	if r.ExitCode() != engine.ExitOK || r.Untouched != 1 {
		t.Fatalf("a moved, renamed existing finding must pass and count as untouched; got exit %d, untouched %d, %+v",
			r.ExitCode(), r.Untouched, r.Regressions)
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
func TestRenameWithoutVersionControlIsRecognised(t *testing.T) {
	base := module("\t_ = time.Now()\n")
	head := module("\t_ = time.Now()\n")
	head["store/rows_test.go"] = head["store/store_test.go"]
	delete(head, "store/store_test.go")
	r := check(t, treeSource{base: base, head: head}) // no renames reported by the source
	if r.ExitCode() != engine.ExitOK {
		t.Fatalf("a file moved with unchanged content must keep its existing findings, got %+v", r.Regressions)
	}
}

// viewpoint: boundaries and errors
// target: engine.Engine.Check
func TestDuplicateFindingsAreMatchedByCount(t *testing.T) {
	once := module("\t_ = time.Now()\n")
	twice := module("\t_ = time.Now()\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: twice, head: once}); r.ExitCode() != engine.ExitOK {
		t.Fatalf("fewer duplicates than base must pass, got %+v", r.Regressions)
	}
	if r := check(t, treeSource{base: once, head: twice}); r.ExitCode() != engine.ExitRegression {
		t.Fatalf("more duplicates than base must fail, got exit %d", r.ExitCode())
	}
}

// viewpoint: boundaries and errors
// target: engine.Engine.Check
func TestMarkers(t *testing.T) {
	plain := module("")
	cases := []struct {
		name string
		head string
		exit int
		rule string
	}{
		{"a reasoned exception suppresses the finding and needs review", "\t// gate:allow tests/no-clock reason: measures wall time on purpose\n\t_ = time.Now()\n", engine.ExitReview, ""},
		{"an exception without a reason has no effect", "\t// gate:allow tests/no-clock\n\t_ = time.Now()\n", engine.ExitRegression, "engine/marker-without-reason"},
		{"an exception for an unknown rule is a regression", "\t// gate:allow tests/no-such-rule reason: x\n", engine.ExitRegression, "engine/marker-unknown-rule"},
		{"an exception that suppresses nothing is a regression", "\t// gate:allow tests/no-clock reason: x\n", engine.ExitRegression, "engine/marker-unused"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := check(t, treeSource{base: plain, head: module(c.head)})
			if r.ExitCode() != c.exit {
				t.Fatalf("want exit %d, got %d: %+v", c.exit, r.ExitCode(), r)
			}
			if c.rule != "" && !hasRule(r.Regressions, c.rule) {
				t.Fatalf("want a %s regression, got %+v", c.rule, r.Regressions)
			}
		})
	}
}

// viewpoint: state integrity
// target: engine.Engine.Check
func TestExceptionNeedsReviewOnlyInTheChangeThatAddsIt(t *testing.T) {
	excepted := module("\t// gate:allow tests/no-clock reason: measures wall time on purpose\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: excepted, head: excepted}); r.ExitCode() != engine.ExitOK {
		t.Fatalf("an exception already in base must not be reported again, got exit %d", r.ExitCode())
	}
	reworded := module("\t// gate:allow tests/no-clock reason: something else\n\t_ = time.Now()\n")
	if r := check(t, treeSource{base: excepted, head: reworded}); r.ExitCode() != engine.ExitReview {
		t.Fatalf("a changed reason must be reviewed again, got exit %d", r.ExitCode())
	}
}

// viewpoint: external contract
// target: engine.Engine.Check with size/file-lines
// A violation that was already in base and grew is reported with both values, so the reader sees
// how much the change added, not only where the file stands now.
func TestAGrownViolationShowsItsBaseValue(t *testing.T) {
	file := func(n int) fstest.MapFS {
		return fstest.MapFS{
			"go.mod":         {Data: []byte("module example.com/app\n")},
			".hadome.yml":    {Data: []byte("schema: 1\nrules:\n  size:\n    file-lines: 5\n")},
			"store/store.go": {Data: []byte("package store\n" + strings.Repeat("// a row\n", n-1))},
		}
	}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: size.All()}
	r, err := e.Check(treeSource{base: file(7), head: file(9)})
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Regressions) != 1 {
		t.Fatalf("want one regression, got %+v", r.Regressions)
	}
	for lang, want := range map[string]string{
		"en": "store/store.go: lines 9, limit 5; grew from 7",
		"ja": "store/store.go: 行数 9（上限 5）。base の 7 から増えた",
	} {
		if got := r.Regressions[0].Message.In(lang); got != want {
			t.Errorf("%s: got %q, want %q", lang, got, want)
		}
	}
}

// viewpoint: external contract
// target: engine.Engine.Check with size/file-lines, engine.WriteJSON and engine.WriteText
// A file five lines over a limit of 300 is reported at line 301, the first line over the limit, in
// every language hadome reads (tree-sitter's only with cgo), in JSON and in text alike. A file hadome could not read is not
// measured, so file-lines does not report it.
func TestAFileOverTheLimitPointsAtItsFirstLineOverIt(t *testing.T) {
	pad := strings.Repeat("\n", 304) // with the first line, each file has 305 lines
	over := map[string]string{
		"store/store.go":           "package store\n" + pad,
		"main.tf":                  "variable \"rows\" {}\n" + pad,
		"roles/web/tasks/main.yml": "- name: ping\n" + pad,
	}
	for p, first := range treeSitterFiles {
		over[p] = first + pad
	}
	head := fstest.MapFS{
		"go.mod":          {Data: []byte("module example.com/app\n")},
		".hadome.yml":     {Data: []byte("schema: 1\nrules:\n  size:\n    file-lines: 300\n")},
		"store/broken.go": {Data: []byte("package store\n\nfunc Append( {\n" + pad)},
	}
	for p, src := range over {
		head[p] = &fstest.MapFile{Data: []byte(src)}
	}
	base := fstest.MapFS{"go.mod": head["go.mod"], ".hadome.yml": head[".hadome.yml"]}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: size.All()}
	r, err := e.Check(treeSource{base: base, head: head})
	if err != nil {
		t.Fatal(err)
	}
	var js bytes.Buffer
	if err := engine.WriteJSON(&js, r, "en"); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Regressions []struct {
			Rule, File string
			Line       int
		} `json:"regressions"`
	}
	if err := json.Unmarshal(js.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	lines := map[string]int{}
	for _, f := range got.Regressions {
		if f.Rule == "size/file-lines" {
			lines[f.File] = f.Line
		}
	}
	var text bytes.Buffer
	engine.WriteText(&text, r, "en")
	for p := range over {
		if lines[p] != 301 {
			t.Errorf("json: %s at line %d, want 301", p, lines[p])
		}
		if !strings.Contains(text.String(), "\n  "+p+":301\n") {
			t.Errorf("text: want %s:301\n%s", p, text.String())
		}
	}
	if _, ok := lines["store/broken.go"]; ok || len(lines) != len(over) {
		t.Errorf("want file-lines for exactly the %d files hadome read, got %v", len(over), lines)
	}
}

// viewpoint: behaviour
// target: engine.Engine.Check
// A marker covers a finding on its own line and on the next one, and none further away: a marker two
// lines above the finding suppresses nothing.
func TestAMarkerCoversItsLineAndTheNext(t *testing.T) {
	const allow = "// gate:allow tests/no-clock reason: measures wall time on purpose"
	for name, tc := range map[string]struct {
		test    string
		covered bool
	}{
		"same line": {"\t_ = time.Now() " + allow + "\n", true},
		"next line": {"\t" + allow + "\n\t_ = time.Now()\n", true},
		"two lines": {"\t" + allow + "\n\n\t_ = time.Now()\n", false},
	} {
		r := check(t, treeSource{base: module(""), head: module(tc.test)})
		if got := !hasRule(r.Regressions, "tests/no-clock") && !hasRule(r.Regressions, "engine/marker-unused"); got != tc.covered {
			t.Errorf("%s: covered %v, want %v; regressions %+v", name, got, tc.covered, r.Regressions)
		}
	}
}

// topCase is a file that starts with src and holds a marker for size/file-lines; top says whether
// the marker is at the top of the file.
type topCase struct {
	path, src string
	top       bool
}

// marker is a valid exception marker for size/file-lines, without its comment delimiters.
const marker = "gate:allow size/file-lines reason: one table of cases"

// viewpoint: behaviour
// target: engine.Engine.Check with size/file-lines
// A marker covers a whole-file finding only at the top of the file: on a comment line that only
// blank lines, comment lines (a license header, //go:build), a shebang and YAML's --- precede. Below
// a package line or code, or after code on its own line, it covers nothing.
func TestAMarkerAtTheTopCoversTheWholeFile(t *testing.T) {
	cases := append([]topCase{
		{"store/license.go", "// Copyright A.\n// License B.\n\n//go:build linux\n\n// " + marker + "\npackage store\n", true},
		{"store/block.go", "/*\n Copyright A.\n*/\n\n// " + marker + "\npackage store\n", true},
		{"store/package.go", "package store\n\n// " + marker + "\n", false},
		{"store/trailing.go", "package store // " + marker + "\n", false},
		{"main.tf", "# Copyright A.\n\n/* " + marker + " */\nvariable \"rows\" {}\n", true},
		{"license.tf", "/*\n Copyright A.\n*/\n# " + marker + "\nvariable \"keys\" {}\n", true},
		{"after.tf", "variable \"cols\" {}\n# " + marker + "\n", false},
		{"roles/web/tasks/main.yml", "---\n# Copyright A.\n# " + marker + "\n- name: ping\n  ping:\n", true},
		{"roles/db/tasks/main.yml", "- name: pong\n  ping:\n# " + marker + "\n", false},
	}, treeSitterTop...)
	head := fstest.MapFS{
		"go.mod":      {Data: []byte("module example.com/app\n")},
		".hadome.yml": {Data: []byte("schema: 1\nrules:\n  size:\n    file-lines: 10\n")},
	}
	for _, c := range cases {
		head[c.path] = &fstest.MapFile{Data: []byte(c.src + strings.Repeat("\n", 20))}
	}
	base := fstest.MapFS{"go.mod": head["go.mod"], ".hadome.yml": head[".hadome.yml"]}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: size.All()}
	r, err := e.Check(treeSource{base: base, head: head})
	if err != nil {
		t.Fatal(err)
	}
	reported := map[string]bool{}
	for _, f := range r.Regressions {
		if f.Rule == "size/file-lines" {
			reported[f.File] = true
		}
	}
	for _, c := range cases {
		if reported[c.path] == c.top {
			t.Errorf("%s: file-lines reported %v, want %v", c.path, reported[c.path], !c.top)
		}
	}
}

func hasRule(fs []engine.Finding, rule string) bool {
	for _, f := range fs {
		if f.Rule == rule {
			return true
		}
	}
	return false
}

// viewpoint: regression
// target: engine.Engine.SelfTest
// guards: every loaded rule has self-test cases, and all of them pass
func TestEveryRuleSelfTestPasses(t *testing.T) {
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	for _, f := range e.SelfTest() {
		t.Errorf("%s: %s: %s", f.Rule, f.Name, f.Err)
	}
}

// viewpoint: behaviour
// target: engine.LossExplainer
// A unit that stops being pure is reported at the import in head that makes it impure, naming what
// that import reaches, directly or through the unit of the repository it imports.
func TestPurityLostNamesWhatMadeTheUnitImpure(t *testing.T) {
	base := module("")
	direct := module("")
	direct["store/save.go"] = &fstest.MapFile{Data: []byte("package store\n\nimport \"database/sql\"\n\nvar _ *sql.DB\n")}
	through := module("")
	through["store/save.go"] = &fstest.MapFile{Data: []byte("package store\n\nimport \"example.com/app/db\"\n\nvar _ = db.Open\n")}
	through["db/db.go"] = &fstest.MapFile{Data: []byte("package db\n\nimport \"os\"\n\n// Open opens a file.\nfunc Open() { _, _ = os.Open(\"x\") }\n")}
	for _, c := range []struct {
		name string
		head fstest.MapFS
		want string
	}{
		{"direct", direct, "example.com/app/store is no longer pure: it imports database/sql, which performs input or output"},
		{"through", through, "example.com/app/store is no longer pure: it imports example.com/app/db, which imports os, which performs input or output"},
	} {
		r := check(t, treeSource{base: base, head: c.head})
		var got []string
		for _, f := range r.Reviews {
			if f.Rule == "tests/purity-lost" {
				got = append(got, fmt.Sprintf("%s:%d %s", f.File, f.Line, f.Message.In("en")))
			}
		}
		if want := "store/save.go:3 " + c.want; len(got) != 1 || got[0] != want {
			t.Errorf("%s: want [%s], got %q", c.name, want, got)
		}
	}
}

// viewpoint: invariant
// target: engine.Engine.Check
// Design "Content hashes": in a check, rename detection and the analyzer's reuse share one hash of
// every file. A file no language reads, and that moved between the versions, is read once in each
// version, for that hash.
func TestACheckHashesEveryFileOnce(t *testing.T) {
	t.Parallel()
	base := module("")
	base["notes/old.txt"] = &fstest.MapFile{Data: []byte("kept as it was\n")}
	head := module("")
	head["notes/new.txt"] = &fstest.MapFile{Data: []byte("kept as it was\n")}
	bc, hc := newCountingFS(base), newCountingFS(head)
	e := &engine.Engine{Analyzer: adapter.New(), Rules: tests.All()}
	if _, err := e.Check(treeSource{base: bc, head: hc}); err != nil {
		t.Fatal(err)
	}
	if n := bc.count("notes/old.txt"); n != 1 {
		t.Errorf("base's notes/old.txt was read %d times, want 1", n)
	}
	if n := hc.count("notes/new.txt"); n != 1 {
		t.Errorf("head's notes/new.txt was read %d times, want 1", n)
	}
}

// countingFS is a file system that counts how often each file's content is read.
type countingFS struct {
	fstest.MapFS
	mu    sync.Mutex
	reads map[string]int
}

func newCountingFS(files fstest.MapFS) *countingFS {
	return &countingFS{MapFS: files, reads: map[string]int{}}
}

func (c *countingFS) Open(name string) (fs.File, error) {
	c.mu.Lock()
	c.reads[name]++
	c.mu.Unlock()
	return c.MapFS.Open(name)
}

func (c *countingFS) ReadFile(name string) ([]byte, error) {
	c.mu.Lock()
	c.reads[name]++
	c.mu.Unlock()
	return c.MapFS.ReadFile(name)
}

func (c *countingFS) count(name string) int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.reads[name]
}
