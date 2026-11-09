package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
	"testing/fstest"
)

func product(yml string) fstest.MapFS {
	return fstest.MapFS{FileName: {Data: []byte(yml)}}
}

func mustLoad(t *testing.T, yml, workdir string) Config {
	t.Helper()
	c, err := Load(product(yml), workdir)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	return c
}

// loadErrors returns the validation errors, failing the test if loading did not fail with them.
func loadErrors(t *testing.T, yml, workdir string) Errors {
	t.Helper()
	_, err := Load(product(yml), workdir)
	var es Errors
	if !errors.As(err, &es) {
		t.Fatalf("Load succeeded or failed without validation errors: %v", err)
	}
	return es
}

const full = `schema: 1
languages: [go, typescript]
invariants:
  - type: idempotent
    target: api.CreatePost
  - type: round-trip
    targets: [store.Export, store.Import]
  - id: billing-1
    says: "Totals match"
rules:
  tests:
    hazards:
      sleep: [acme/testutil.Wait]
    io: [example.com/acme/db]
gates:
  - id: acme/log-append-only
    kind: finding
    run: [scripts/gates/log-append-only]
    paths: ["internal/store/**"]
    timeout: 30s
    options:
      forbidden: [UPDATE, DELETE]
      limit: 3
`

// Invariant 1 of docs/design/configuration.md: with no configuration file, hadome runs.
func TestNoFileMeansBuiltInSettings(t *testing.T) {
	c, err := Load(fstest.MapFS{}, t.TempDir())
	if err != nil {
		t.Fatalf("Load without a file must not fail: %v", err)
	}
	if c.Schema != 1 || len(c.Layers) != 0 || len(c.Invariants) != 0 {
		t.Fatalf("want built-in settings only, got %+v", c)
	}
}

func TestEveryDocumentedKeyIsRead(t *testing.T) {
	c := mustLoad(t, full, t.TempDir())
	var keys []string
	for _, i := range c.Invariants {
		keys = append(keys, i.Key())
	}
	if got := strings.Join(keys, "|"); got != "idempotent api.CreatePost|round-trip store.Export,store.Import|billing-1" {
		t.Errorf("invariants: %s", got)
	}
	if len(c.Gates) != 1 || c.Gates[0].ID != "acme/log-append-only" || c.Gates[0].Timeout != "30s" ||
		strings.Join(c.Gates[0].Paths, ",") != "internal/store/**" {
		t.Errorf("gates: %+v", c.Gates)
	}
	if got := fmt.Sprint(c.Gates[0].Options); got != "map[forbidden:[UPDATE DELETE] limit:3]" {
		t.Errorf("options are passed on untouched, got %s", got)
	}
	if strings.Join(c.Hazards["sleep"], ",") != "acme/testutil.Wait" || strings.Join(c.IO, ",") != "example.com/acme/db" {
		t.Errorf("rules: hazards %v io %v", c.Hazards, c.IO)
	}
	if strings.Join(c.Languages, ",") != "go,typescript" || strings.Join(c.Layers, ",") != FileName {
		t.Errorf("languages %v layers %v", c.Languages, c.Layers)
	}
}

// Invariant 2 of docs/design/configuration.md: a configuration that does not match the schema never runs a check.
// Each error must say where, what is wrong and what would be right.
func TestErrorsNameTheKeyTheLineAndAFix(t *testing.T) {
	cases := []struct {
		name, yml, key string
		line           int
		mention        string // must appear in the allowed values or the example
	}{
		{"unknown key", "schema: 1\nlanguage: [go]\n", "language", 2, "languages"},
		{"unknown nested key", "schema: 1\nrules:\n  tests:\n    i0: []\n", "rules.tests.i0", 4, "hazards, io"},
		{"string where an integer goes", "schema: \"1\"\n", "schema", 1, "schema: 1"},
		{"unknown schema version", "schema: 7\n", "schema", 1, "1"},
		{"unknown invariant type", "schema: 1\ninvariants:\n  - type: idempotant\n    target: a.B\n", "invariants[0].type", 3, "idempotent"},
		{"both target and targets", "schema: 1\ninvariants:\n  - type: idempotent\n    target: a.B\n    targets: [a.B, a.C]\n",
			"invariants[0]", 3, "target"},
		{"neither shape", "schema: 1\ninvariants:\n  - target: a.B\n", "invariants[0]", 3, "type"},
		{"missing schema", "languages: [go]\n", "schema", 1, "schema: 1"},
		{"gate without a namespace", "schema: 1\ngates:\n  - id: mine\n    kind: finding\n    run: [x]\n", "gates[0].id", 3, "acme/"},
		{"list where a mapping goes", "schema: 1\nrules: [tests]\n", "rules", 2, "rules:"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			es := loadErrors(t, tc.yml, t.TempDir())
			for _, e := range es {
				if e.Key == tc.key {
					if e.Line != tc.line || e.File != FileName {
						t.Errorf("at %s:%d, want %s:%d", e.File, e.Line, FileName, tc.line)
					}
					if !strings.Contains(e.Allowed+e.Example, tc.mention) {
						t.Errorf("error does not show %q: %v", tc.mention, e)
					}
					return
				}
			}
			t.Errorf("no error for key %s: %v", tc.key, es)
		})
	}
}

func TestAllErrorsAreReportedTogether(t *testing.T) {
	es := loadErrors(t, "schema: 2\nlanguages: [cobol]\nextra: 1\n", t.TempDir())
	lines := map[int]bool{}
	for _, e := range es {
		lines[e.Line] = true
	}
	if len(lines) != 3 {
		t.Fatalf("want errors on lines 1, 2 and 3, got %v", es)
	}
}

func TestInvalidYAMLIsAnErrorNotAPanic(t *testing.T) {
	es := loadErrors(t, "schema: [\n", t.TempDir())
	if es[0].Key != "(file)" {
		t.Fatalf("got %v", es)
	}
}

func write(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestSharedLayersAreMergedUnderTheProduct(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "shared", "base.yml"), `schema: 1
languages: [go, python]
invariants:
  - id: shared-1
    says: "Shared promise"
rules:
  tests:
    io: [example.com/shared/db]
`)
	c := mustLoad(t, `schema: 1
extends: [shared/base.yml]
languages: [go]
invariants:
  - type: erasable
    target: store.Delete
rules:
  tests:
    io: [example.com/acme/db]
`, dir)
	var keys []string
	for _, i := range c.Invariants {
		keys = append(keys, i.Key())
	}
	if strings.Join(keys, ",") != "shared-1,erasable store.Delete" {
		t.Errorf("invariants from both layers, shared first: %v", keys)
	}
	if strings.Join(c.Languages, ",") != "go" {
		t.Errorf("the product's languages replace the shared ones: %v", c.Languages)
	}
	if strings.Join(c.IO, ",") != "example.com/shared/db,example.com/acme/db" {
		t.Errorf("added table entries from both layers: %v", c.IO)
	}
	if len(c.Layers) != 2 || c.Layers[1] != FileName {
		t.Errorf("layers, earliest first: %v", c.Layers)
	}
}

// Invariant 4 of docs/design/configuration.md: hadome never reads configuration from the network.
// guards: no-network
func TestExtendsRefusesURLsCyclesAndMissingFiles(t *testing.T) {
	dir := t.TempDir()
	write(t, filepath.Join(dir, "a.yml"), "schema: 1\nextends: [b.yml]\n")
	write(t, filepath.Join(dir, "b.yml"), "schema: 1\nextends: [a.yml]\n")
	cases := map[string]string{
		"url":     "https://example.com/hadome.yml",
		"cycle":   "a.yml",
		"missing": "nowhere.yml",
	}
	for name, ref := range cases {
		t.Run(name, func(t *testing.T) {
			yml := "schema: 1\nlanguages: [go]\nextends:\n  - " + ref + "\n"
			es := loadErrors(t, yml, dir)
			if len(es) != 1 || es[0].Key != "extends" {
				t.Fatalf("got %v", es)
			}
			if name != "cycle" && (es[0].File != FileName || es[0].Line != 4) {
				t.Errorf("points at %s:%d, want the extends entry at %s:4", es[0].File, es[0].Line, FileName)
			}
		})
	}
}

func looser(changes []Change) []string {
	var out []string
	for _, c := range changes {
		if c.Looser {
			out = append(out, c.Key+" -"+c.Removed)
		}
	}
	return out
}

// Invariant 5 of docs/design/configuration.md: a change cannot loosen the rules that judge it without review.
func TestDiffMarksLooseningOnly(t *testing.T) {
	dir := t.TempDir()
	base := mustLoad(t, full, dir)
	cases := []struct {
		name, head string
		wantLooser int
	}{
		{"unchanged", full, 0},
		{"adding an invariant", strings.Replace(full, "invariants:\n", "invariants:\n  - id: new-1\n    says: x\n", 1), 0},
		{"removing an invariant", strings.Replace(full, "  - id: billing-1\n    says: \"Totals match\"\n", "", 1), 1},
		{"removing a gate", full[:strings.Index(full, "gates:")], 1},
		{"removing an io entry", strings.Replace(full, "io: [example.com/acme/db]", "io: []", 1), 1},
		{"adding an assertion", strings.Replace(full, "io: [example.com/acme/db]", "io: [example.com/acme/db]\n    assertions: [acme.MustEqual]", 1), 1},
		{"narrowing languages", strings.Replace(full, "[go, typescript]", "[go]", 1), 1},
		{"changing a gate's options", strings.Replace(full, "limit: 3", "limit: 4", 1), 1},
		{"changing a gate's command", strings.Replace(full, "run: [scripts/gates/log-append-only]", "run: [\"true\"]", 1), 1},
		{"changing what an invariant says", strings.Replace(full, "Totals match", "Totals agree", 1), 0},
		{"omitting languages detects all of them", strings.Replace(full, "languages: [go, typescript]\n", "", 1), 0},
		{"deleting the file", "", 6},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var head Config
			if tc.head == "" {
				head, _ = Load(fstest.MapFS{}, dir)
			} else {
				head = mustLoad(t, tc.head, dir)
			}
			if got := looser(Diff(base, head)); len(got) != tc.wantLooser {
				t.Errorf("want %d looser changes, got %v", tc.wantLooser, got)
			}
		})
	}
}

// Invariant 3 of docs/design/configuration.md: the published schema is generated, never edited by hand.
func TestPublishedSchemaIsGenerated(t *testing.T) {
	published, err := os.ReadFile(filepath.Join("..", "..", "schema", "hadome.schema.json"))
	if err != nil {
		t.Fatalf("reading the published schema: %v", err)
	}
	if string(published) != string(JSONSchema()) {
		t.Fatal("schema/hadome.schema.json differs from the specification; regenerate it with `hadome schema > schema/hadome.schema.json`")
	}
}

func TestExplainKnowsKeysAndValues(t *testing.T) {
	for _, name := range []string{"invariants", "invariants.target", "rules.tests.io", "idempotent", "typescript", "gates.kind"} {
		if text, ok := Explain(name); !ok || !strings.HasPrefix(text, name+" — ") {
			t.Errorf("Explain(%q) = %q, %v", name, text, ok)
		}
	}
	if _, ok := Explain("nonsense"); ok {
		t.Error("an unknown name must not be explained")
	}
}

func TestMisspeltKeysGetASuggestion(t *testing.T) {
	es := loadErrors(t, "schema: 1\ninvarients: []\nzzz: 1\n", t.TempDir())
	if !strings.Contains(es[0].Problem, "did you mean invariants?") {
		t.Errorf("a near miss gets a suggestion: %v", es[0])
	}
	if strings.Contains(es[1].Problem, "did you mean") {
		t.Errorf("an unrelated key gets none: %v", es[1])
	}
}

// Invariant 5 of docs/design/configuration.md, for limits: raising one, or dropping a lowered one back to its
// default, loosens; lowering never does.
func TestLimitsDefaultAndLoosenUpward(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	if none.Size["function-lines"] != 60 || none.Size["file-lines"] != 400 {
		t.Fatalf("defaults fill omitted limits: %v", none.Size)
	}
	lower := mustLoad(t, "schema: 1\nrules:\n  size:\n    function-lines: 40\n", dir)
	higher := mustLoad(t, "schema: 1\nrules:\n  size:\n    function-lines: 80\n", dir)
	cases := []struct {
		name       string
		base, head Config
		want       int
	}{
		{"lowering", none, lower, 0},
		{"raising", none, higher, 1},
		{"dropping a lowered limit", lower, none, 1},
		{"unchanged default written out", none, mustLoad(t, "schema: 1\nrules:\n  size:\n    function-lines: 60\n", dir), 0},
	}
	for _, tc := range cases {
		if got := looser(Diff(tc.base, tc.head)); len(got) != tc.want {
			t.Errorf("%s: want %d looser changes, got %v", tc.name, tc.want, got)
		}
	}
	if es := loadErrors(t, "schema: 1\nrules:\n  size:\n    params: 0\n", dir); es[0].Key != "rules.size.params" {
		t.Errorf("a limit below 1 is an error: %v", es)
	}
}

// viewpoint: behaviour
// target: config.Diff
// An assertion entry excuses tests, so adding one loosens the rules and removing one does not.
func TestAddingAnAssertionLoosens(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	one := mustLoad(t, "schema: 1\nrules:\n  tests:\n    assertions: [acme.MustEqual]\n", dir)
	if got := looser(Diff(none, one)); len(got) != 1 {
		t.Errorf("adding: want 1 looser change, got %v", got)
	}
	if got := looser(Diff(one, none)); len(got) != 0 {
		t.Errorf("removing: want no looser change, got %v", got)
	}
}

func TestTheTodoFormIsARegularExpressionAndChangingItNeedsReview(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	if none.TodoForm != DefaultTodo {
		t.Fatalf("the default form applies when omitted: %q", none.TodoForm)
	}
	own := mustLoad(t, "schema: 1\nrules:\n  comments:\n    todo: '^TODO\\(stage[0-9]+\\): \\S'\n", dir)
	if own.TodoForm != `^TODO\(stage[0-9]+\): \S` {
		t.Fatalf("got %q", own.TodoForm)
	}
	if got := looser(Diff(none, own)); len(got) != 1 {
		t.Errorf("changing the form, in either direction, needs review: %v", got)
	}
	if got := looser(Diff(own, own)); len(got) != 0 {
		t.Errorf("an unchanged form needs none: %v", got)
	}
	if es := loadErrors(t, "schema: 1\nrules:\n  comments:\n    todo: 'TODO('\n", dir); es[0].Key != "rules.comments.todo" {
		t.Errorf("an invalid regular expression is an error: %v", es)
	}
}

// An error names how to write what is wrong, so every key in the specification carries an example.
func TestEveryKeyHasAnExample(t *testing.T) {
	var walk func(k Key, path string)
	walk = func(k Key, path string) {
		if exampleOf(k) == "" {
			t.Errorf("%s has no example", displayPath(path))
		}
		if k.Kind == KindList && k.Elem != nil {
			k = *k.Elem
		}
		fields := k.Fields
		for _, v := range k.Variants {
			fields = append(fields, v.Fields...)
		}
		for _, f := range fields {
			walk(f, joinPath(path, f.Name))
		}
	}
	walk(Spec, "")
}

// A misspelt key shows the example of the key it most likely meant; an unrelated one shows how the
// enclosing mapping is written.
func TestAMisspeltKeyShowsTheExampleOfTheKeyItMeant(t *testing.T) {
	es := loadErrors(t, "schema: 1\ninvariant: []\nzzz: 1\n", t.TempDir())
	if len(es) != 2 {
		t.Fatalf("want two errors, got %v", es)
	}
	if !strings.HasPrefix(es[0].Example, "invariants:") {
		t.Errorf("a near miss shows how invariants is written: %q", es[0].Example)
	}
	if !strings.HasPrefix(es[1].Example, "schema: 1") {
		t.Errorf("an unrelated key shows how the file is written: %q", es[1].Example)
	}
}

// Which files are tests moves what the rules cover both ways, so adding and removing a glob both
// need review; a generated glob takes files out of rules, so only adding one does.
func TestFileRolesLoosen(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	tests := mustLoad(t, "schema: 1\nfiles:\n  tests: [\"checks/**\"]\n", dir)
	gen := mustLoad(t, "schema: 1\nfiles:\n  generated: [\"gen/**\"]\n", dir)
	if got := looser(Diff(none, tests)); len(got) != 1 {
		t.Errorf("adding a test glob: want 1 looser change, got %v", got)
	}
	if got := looser(Diff(tests, none)); len(got) != 1 {
		t.Errorf("removing a test glob: want 1 looser change, got %v", got)
	}
	if got := looser(Diff(none, gen)); len(got) != 1 {
		t.Errorf("adding a generated glob: want 1 looser change, got %v", got)
	}
	if got := looser(Diff(gen, none)); len(got) != 0 {
		t.Errorf("removing a generated glob: want none, got %v", got)
	}
	if !MatchesAny(tests.TestFiles, "checks/a/b.py") || MatchesAny(tests.TestFiles, "src/checks.py") {
		t.Errorf("checks/** matches everything under checks/ and nothing else: %v", tests.TestFiles)
	}
}

// A glob path.Match cannot read would match nothing without a word, so it is a configuration error.
func TestAMalformedGlobIsAnError(t *testing.T) {
	es := loadErrors(t, "schema: 1\nfiles:\n  generated: [\"gen/[a\"]\n", t.TempDir())
	if len(es) != 1 || !strings.Contains(es[0].Problem, "not a valid glob") {
		t.Fatalf("got %v", es)
	}
}

// A change points at where it is in head, so a person reviewing it, or an annotation on a pull
// request, lands on the line: the added list entry, a changed key, or the arrow in the diagram.
func TestChangesSayWhereTheyAre(t *testing.T) {
	base := mustLoadTree(t, fstest.MapFS{
		".hadome.yml":  {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md": {Data: []byte("```mermaid\nflowchart TB\n  a[\"a\"] --> b[\"b\"]\n```\n")},
	})
	head := mustLoadTree(t, fstest.MapFS{
		".hadome.yml":  {Data: []byte("schema: 1\nrules:\n  size:\n    params: 6\n  tests:\n    assertions:\n      - acme.Equal\n      - acme.Same\n  deps:\n    diagram: docs/deps.md\n")},
		"docs/deps.md": {Data: []byte("```mermaid\nflowchart TB\n  a[\"a\"] --> b[\"b\"]\n  a --> c[\"c\"]\n```\n")},
	})
	var got []string
	for _, c := range Diff(base, head) {
		if c.Looser {
			got = append(got, fmt.Sprintf("%s:%d %s +%s", c.File, c.Line, c.Key, c.Added))
		}
	}
	sort.Strings(got)
	want := strings.Join([]string{
		".hadome.yml:4 rules.size.params +6",
		".hadome.yml:7 rules.tests.assertions +acme.Equal",
		".hadome.yml:8 rules.tests.assertions +acme.Same",
		"docs/deps.md:4 rules.deps.diagram +a --> c",
	}, "\n")
	if g := strings.Join(got, "\n"); g != want {
		t.Errorf("got:\n%s\n\nwant:\n%s", g, want)
	}
}

// A command is a sequence: its arguments mean something only in their order, so a change shows the
// command whole, as written, and never sorts its arguments.
func TestACommandChangeShowsTheCommandInItsOrder(t *testing.T) {
	dir := t.TempDir()
	changes := Diff(mustLoad(t, "schema: 1\n", dir), mustLoad(t, "schema: 1\nmutation:\n  test: [npm, run, test, --, --run]\n", dir))
	var got []string
	for _, c := range changes {
		got = append(got, c.Key+" +"+c.Added)
	}
	if want := "mutation.test +[npm, run, test, --, --run]"; !slices.Contains(got, want) || len(got) != 1 {
		t.Errorf("got %v, want only %q", got, want)
	}
}

// Invariant 5 of docs/design/configuration.md: without a mutation section hadome mutate does not run,
// so adding one never loosens anything; removing or weakening one that base has does.
func TestAddingTheMutationSectionIsNotLoosening(t *testing.T) {
	dir := t.TempDir()
	const without = "schema: 1\n"
	const with = "schema: 1\nmutation:\n  test: [go, test, ./...]\n  max-mutants: 30\n"
	cases := []struct {
		name, base, head string
		want             []string
	}{
		{"adding the section", without, with, nil},
		{"removing the section", with, without, []string{"mutation.test -[go, test, ./...]"}},
		{"lowering the limit", with, strings.Replace(with, "30", "10", 1), []string{"mutation.max-mutants -30"}},
		{"changing the command", with, strings.Replace(with, "./...", "./store", 1), []string{"mutation.test -[go, test, ./...]"}},
		{"reordering the arguments", with, strings.Replace(with, "test, ./...", "./..., test", 1), []string{"mutation.test -[go, test, ./...]"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := looser(Diff(mustLoad(t, tc.base, dir), mustLoad(t, tc.head, dir)))
			sort.Strings(got)
			if strings.Join(got, "\n") != strings.Join(tc.want, "\n") {
				t.Errorf("looser changes:\n%s\n\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tc.want, "\n"))
			}
		})
	}
}

// {packages} becomes one element per package and only Go tests fill it, so it must be an element by
// itself, written once, in a configuration whose languages include Go when it names them. Anything
// else stops the check instead of narrowing nothing.
func TestPackagesPlaceholderIsAWholeElementForGo(t *testing.T) {
	dir := t.TempDir()
	for _, tc := range []struct {
		name, yml, key string
		line           int
	}{
		{"inside an element", "schema: 1\nmutation:\n  test: [go, test, \"./{packages}/...\"]\n", "mutation.test[2]", 3},
		{"twice", "schema: 1\nmutation:\n  test: [go, test, \"{packages}\", \"{packages}\"]\n", "mutation.test[3]", 3},
		{"languages without go", "schema: 1\nlanguages: [python]\nmutation:\n  test: [pytest, \"{packages}\"]\n", "mutation.test[1]", 4},
	} {
		t.Run(tc.name, func(t *testing.T) {
			es := loadErrors(t, tc.yml, dir)
			if len(es) != 1 || es[0].Key != tc.key || es[0].Line != tc.line {
				t.Errorf("want one error at %s on line %d, got %v", tc.key, tc.line, es)
			}
		})
	}
	for _, ok := range []string{
		"schema: 1\nmutation:\n  test: [go, test, \"{packages}\", -run, \"^({tests})$\"]\n",
		"schema: 1\nlanguages: [go, python]\nmutation:\n  test: [go, test, \"{packages}\"]\n",
	} {
		mustLoad(t, ok, dir)
	}
}

// Re-classifying test support as product code moves what the rules cover both ways: the files
// enter the size and comment rules, but tests stop counting the hazards in them as their own. So
// adding and removing a product glob both need review.
func TestProductFilesLoosenBothWays(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	prod := mustLoad(t, "schema: 1\nfiles:\n  product: [\"e2e/server/**\"]\n", dir)
	if got := looser(Diff(none, prod)); strings.Join(got, "|") != "files.product -" {
		t.Errorf("adding a product glob: want 1 looser change of files.product, got %v", got)
	}
	if got := looser(Diff(prod, none)); strings.Join(got, "|") != "files.product -e2e/server/**" {
		t.Errorf("removing a product glob: want 1 looser change, got %v", got)
	}
	if !MatchesAny(prod.ProductFiles, "e2e/server/main.ts") || MatchesAny(prod.ProductFiles, "e2e/fixtures.ts") {
		t.Errorf("e2e/server/** matches only what is under e2e/server/: %v", prod.ProductFiles)
	}
	if es := loadErrors(t, "schema: 1\nfiles:\n  product: [\"e2e/[a\"]\n", dir); es[0].Key != "files.product[0]" {
		t.Errorf("a malformed product glob is an error: %v", es)
	}
}

// An entry point lets symbols off their doc comment, so adding one, or widening one, needs review;
// removing one asks for more doc comments and does not.
func TestEntryPointsLoosenWhenAdded(t *testing.T) {
	dir := t.TempDir()
	none := mustLoad(t, "schema: 1\n", dir)
	narrow := mustLoad(t, "schema: 1\nrules:\n  comments:\n    entry-points:\n      - names: [load]\n        paths: [\"**/+page*.ts\"]\n", dir)
	wide := mustLoad(t, "schema: 1\nrules:\n  comments:\n    entry-points:\n      - names: [load]\n", dir)
	if got := looser(Diff(none, narrow)); len(got) != 1 || !strings.HasPrefix(got[0], "rules.comments.entry-points ") {
		t.Errorf("adding an entry point: want 1 looser change, got %v", got)
	}
	if got := looser(Diff(narrow, none)); len(got) != 0 {
		t.Errorf("removing an entry point: want none, got %v", got)
	}
	if got := looser(Diff(narrow, wide)); len(got) != 1 {
		t.Errorf("dropping an entry point's paths widens it: want 1 looser change, got %v", got)
	}
	e := narrow.EntryPoints
	if len(e) != 1 || !e[0].Matches("load", "web/src/routes/+page.server.ts") || e[0].Matches("load", "web/src/lib/data.ts") ||
		e[0].Matches("loader", "web/src/routes/+page.ts") {
		t.Errorf("load in +page*.ts files only: %+v", e)
	}
	if len(wide.EntryPoints) != 1 || !wide.EntryPoints[0].Matches("load", "any/file.ts") {
		t.Errorf("an entry point without paths applies to every file: %+v", wide.EntryPoints)
	}
}

// An entry point without names would let off nothing or everything, so it is an error, as is a
// name glob path.Match cannot read.
func TestAnEntryPointNeedsReadableNames(t *testing.T) {
	dir := t.TempDir()
	for yml, key := range map[string]string{
		"      - paths: [\"functions/**\"]\n": "rules.comments.entry-points[0].names",
		"      - names: []\n":                 "rules.comments.entry-points[0].names",
		"      - names: [\"on[\"]\n":          "rules.comments.entry-points[0].names[0]",
	} {
		es := loadErrors(t, "schema: 1\nrules:\n  comments:\n    entry-points:\n"+yml, dir)
		if es[0].Key != key {
			t.Errorf("%q: want an error at %s, got %v", yml, key, es)
		}
	}
}
