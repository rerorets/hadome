package deps_test

import (
	"fmt"
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/deps"
)

// drafted is a version with units at every depth, including the root, a unit and units under it, a
// directory named end, one starting with a digit, and two whose names come out as the same ID. Its imports and calls go
// between units of one box and of different boxes, and its calls are made by a function, through
// an interface to an implementation in another unit, by entry-point code and by code outside
// functions.
func drafted() *facts.Analysis {
	units := []string{".", "cmd", "cmd/tool/run", "internal/store", "internal/store/content", "internal/port",
		"internal/domain", "end", "web-app/src", "web_app/src", "9lives"}
	a := &facts.Analysis{}
	for _, u := range units {
		a.Files = append(a.Files, facts.File{Path: u + "/f.go", Unit: "m/" + u, UnitPath: u, Role: facts.RoleSource})
	}
	imp := func(from, to string) facts.Import {
		return facts.Import{Path: from + "/f.go", Line: 1, FromUnit: "m/" + from, ToUnit: "m/" + to, Internal: true}
	}
	a.Imports = []facts.Import{imp(".", "cmd"), imp("cmd", "internal/store"), imp("cmd/tool/run", "internal/store/content"),
		imp("internal/store/content", "internal/store"), imp("internal/store", "internal/domain"), imp("end", "web-app/src"),
		{Path: "cmd/f.go", Line: 2, FromUnit: "m/cmd", ToUnit: "fmt"}}
	a.Symbols = []facts.Symbol{
		{ID: "cmd.Main", Path: "cmd/f.go", Line: 1, Calls: []string{"port.Finder.Find"}},
		{ID: "port.Finder", Kind: facts.SymbolInterface, Path: "internal/port/f.go", Line: 1},
		{ID: "port.Finder.Find", Kind: facts.SymbolMethod, Path: "internal/port/f.go", Line: 2},
		{ID: "content.Repo", Kind: facts.SymbolType, Path: "internal/store/content/f.go", Line: 1, Supertypes: []string{"port.Finder"}},
		{ID: "content.Repo.Find", Kind: facts.SymbolMethod, Path: "internal/store/content/f.go", Line: 2, Calls: []string{"domain.Rule"}},
		{ID: "domain.Rule", Path: "internal/domain/f.go", Line: 1},
		{ID: "web.Render", Path: "web-app/src/f.go", Line: 1},
	}
	a.Entries = []facts.Entry{{Path: "end/f.go", Line: 1, Calls: []string{"web.Render"}}}
	a.Files[0].Calls = []string{"cmd.Main"}
	return a
}

// viewpoint: invariant
// target: deps.DraftDiagram, deps.DraftCalls
// Design suggest, invariant 5: with the drafts of a version configured as its diagrams, at any
// depth, the version gets no deps/ finding: every unit is in a box, every box holds a unit, and
// every import and call between boxes has its arrow.
func TestDraftedDiagramsGiveTheirVersionNoFinding(t *testing.T) {
	a := drafted()
	for _, depth := range []int{1, 2, 3} {
		t.Run(fmt.Sprintf("depth %d", depth), func(t *testing.T) {
			tree := fstest.MapFS{
				".hadome.yml":   {Data: []byte("schema: 1\nrules:\n  deps:\n    diagram: docs/deps.md\n    calls: docs/calls.md\n")},
				"docs/deps.md":  {Data: []byte(deps.DraftDiagram(a, depth))},
				"docs/calls.md": {Data: []byte(deps.DraftCalls(a, depth))},
			}
			c, err := config.Load(tree, ".")
			if err != nil {
				t.Fatalf("the drafts do not load as configuration: %v\n%s\n%s", err, tree["docs/deps.md"].Data, tree["docs/calls.md"].Data)
			}
			for _, r := range deps.All() {
				fr, ok := r.(engine.FindingRule)
				if !ok { // deps/cycle looks at imports, not at the diagrams
					continue
				}
				got, err := fr.Find(engine.Input{Facts: a, Config: c})
				if err != nil {
					t.Fatal(err)
				}
				for _, f := range got {
					t.Errorf("%s: %s", r.ID(), f.Message)
				}
			}
		})
	}
}

// viewpoint: behaviour
// target: deps.DraftDiagram
// Design suggest, "Diagram drafts": units are grouped at the depth into boxes, counting only
// directories that branch, a directory with units under it as dir/..., a unit that branches fewer
// times in a box of its own (cmd/tool/run, under directories with one child each), the root's in the box "." (root), and an
// arrow is drawn once between two boxes, never within one; IDs are the labels' letters and digits,
// told apart by a number when two come out the same; boxes and arrows come in the order of their
// text, so the same version always gives the same file.
func TestADraftGroupsUnitsIntoBoxesAtTheDepth(t *testing.T) {
	got := deps.DraftDiagram(drafted(), 2)
	for _, want := range []string{
		"  web_app_src2[\"web_app/src\"]\n", "  u_9lives[\"9lives\"]\n",
		"  root[\".\"]\n", "  cmd[\"cmd\"]\n", "  cmd_tool_run[\"cmd/tool/run\"]\n", "  internal_store[\"internal/store/...\"]\n",
		"  u_end[\"end\"]\n", "  web_app_src[\"web-app/src\"]\n",
		"  root --> cmd\n", "  cmd --> internal_store\n", "  cmd_tool_run --> internal_store\n", "  internal_store --> internal_domain\n",
		"  u_end --> web_app_src\n",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in the draft, got\n%s", want, got)
		}
	}
	for _, unwanted := range []string{"internal_store --> internal_store", "internal/store\"", "fmt"} {
		if strings.Contains(got, unwanted) {
			t.Errorf("want no %q in the draft, got\n%s", unwanted, got)
		}
	}
	if n := strings.Count(got, "-->"); n != 5 {
		t.Errorf("want 5 arrows, got %d in\n%s", n, got)
	}
	if !strings.HasSuffix(got, "\n```\n") {
		t.Errorf("want the mermaid block closed, got\n%s", got)
	}
	var boxes, arrows []string
	for _, l := range strings.Split(got, "\n") {
		switch {
		case strings.Contains(l, " --> "):
			arrows = append(arrows, l)
		case strings.HasPrefix(l, "  "):
			boxes = append(boxes, strings.SplitN(l, "[", 2)[1])
		}
	}
	if !slices.IsSorted(boxes) || !slices.IsSorted(arrows) {
		t.Errorf("want boxes by label and arrows in order, got\n%s", got)
	}
}

// viewpoint: behaviour
// target: deps.DraftCalls
// The call diagram's draft draws the calls as deps/call-not-in-diagram counts them: a call through
// an interface lands on the implementation's unit, and entry-point code and code outside functions
// call from their own units.
func TestACallDraftDrawsCallsAsTheRuleCountsThem(t *testing.T) {
	got := deps.DraftCalls(drafted(), 2)
	for _, want := range []string{"  cmd --> internal_store\n", "  internal_store --> internal_domain\n", "  u_end --> web_app_src\n", "  root --> cmd\n"} {
		if !strings.Contains(got, want) {
			t.Errorf("want %q in the draft, got\n%s", want, got)
		}
	}
	if strings.Contains(got, "internal_port") && strings.Contains(got, "--> internal_port") {
		t.Errorf("want no arrow to the interface's unit, got\n%s", got)
	}
	if !strings.Contains(got, "wiring") {
		t.Errorf("want the note on wiring in the call diagram's draft, got\n%s", got)
	}
}

// viewpoint: behaviour
// target: deps.DraftDiagram
// Design suggest, "Counting levels": directories with one child each, as Java's and Kotlin's
// src/main/kotlin/<package path>, are not counted, so the packages under a long path still get boxes
// of their own; a unit at the top of such a path, such as a build script's, does not count either,
// nor does a split by language (src/main into kotlin, resources and webui), since branching is seen
// among units of one language.
func TestALongUnbranchedPathDoesNotTakeEverythingIntoOneBox(t *testing.T) {
	const pkg = "mod/server/src/main/kotlin/com/example/shop"
	a := &facts.Analysis{}
	unit := func(dir, lang string) {
		a.Files = append(a.Files, facts.File{Path: dir + "/f", Language: lang, Unit: "m/" + dir, UnitPath: dir, Role: facts.RoleSource})
	}
	for _, u := range []string{"mod/server", pkg + "/order", pkg + "/user", pkg + "/user/api", "scripts"} {
		unit(u, "kotlin")
	}
	unit("mod/server/src/main/webui/app", "typescript")
	unit("mod/server/src/main/resources/db", "sql")
	got := deps.DraftDiagram(a, 2)
	for _, want := range []string{"[\"mod/server\"]", "[\"" + pkg + "/order\"]", "[\"" + pkg + "/user/...\"]", "[\"scripts\"]",
		"[\"mod/server/src/main/webui/app\"]"} {
		if !strings.Contains(got, want) {
			t.Errorf("want the box %s in the draft, got\n%s", want, got)
		}
	}
}

// viewpoint: behaviour
// target: deps.DraftDiagram, deps.DraftCalls
// Design suggest, "Arrows both ways": boxes that import (or call) each other are listed after the
// note, once per pair, so that a person decides the direction; both arrows are still drawn.
func TestADraftListsBoxesThatDependOnEachOther(t *testing.T) {
	a := &facts.Analysis{}
	for _, u := range []string{"a", "b", "c", "d", "e"} {
		a.Files = append(a.Files, facts.File{Path: u + "/f.go", Unit: "m/" + u, UnitPath: u, Role: facts.RoleSource})
	}
	imp := func(from, to string) facts.Import {
		return facts.Import{Path: from + "/f.go", Line: 1, FromUnit: "m/" + from, ToUnit: "m/" + to, Internal: true}
	}
	a.Imports = []facts.Import{imp("d", "e"), imp("e", "d"), imp("a", "b"), imp("a", "b"), imp("b", "a"), imp("b", "c")}
	a.Symbols = []facts.Symbol{{ID: "a.F", Path: "a/f.go", Line: 1, Calls: []string{"b.G"}},
		{ID: "b.G", Path: "b/f.go", Line: 1, Calls: []string{"a.F"}}}
	got := deps.DraftDiagram(a, 2)
	if !strings.Contains(got, "import each other") || strings.Count(got, "- `a` ⇄ `b`\n") != 1 || strings.Contains(got, "`b` ⇄ `c`") {
		t.Errorf("want a ⇄ b listed once and b, c not, got\n%s", got)
	}
	if strings.Index(got, "`a` ⇄ `b`") > strings.Index(got, "`d` ⇄ `e`") {
		t.Errorf("want the boxes listed in order of their names, got\n%s", got)
	}
	if !strings.Contains(got, "  a --> b\n") || !strings.Contains(got, "  b --> a\n") {
		t.Errorf("want both arrows drawn, got\n%s", got)
	}
	if calls := deps.DraftCalls(a, 2); !strings.Contains(calls, "call each other") || !strings.Contains(calls, "- `a` ⇄ `b`\n") {
		t.Errorf("want a ⇄ b listed as calling each other, got\n%s", calls)
	}
	if one := deps.DraftDiagram(&facts.Analysis{Files: a.Files, Imports: a.Imports[5:]}, 2); strings.Contains(one, "each other") {
		t.Errorf("want no list when no boxes depend on each other, got\n%s", one)
	}
}

// TestADirectoryOfTwoLanguagesIsGroupedByTheFirstByName pins that a directory holding units of two
// languages is grouped as the first language by name counts it: here Kotlin, whose packages branch
// under kotlin, rather than Python, whose one package never branches and would get a box of its own.
func TestADirectoryOfTwoLanguagesIsGroupedByTheFirstByName(t *testing.T) {
	a := &facts.Analysis{}
	file := func(path, lang string) {
		dir := path[:strings.LastIndex(path, "/")]
		a.Files = append(a.Files, facts.File{Path: path, Language: lang, Unit: lang + ":" + dir, UnitPath: dir, Role: facts.RoleSource})
	}
	file("src/main/kotlin/a/x/X.kt", "kotlin")
	file("src/main/kotlin/a/x/x.py", "python")
	file("src/main/kotlin/a/y/Y.kt", "kotlin")
	file("src/main/kotlin/b/B.kt", "kotlin")
	got := deps.DraftDiagram(a, 1)
	if !strings.Contains(got, "[\"src/main/kotlin/a/...\"]") || strings.Contains(got, "[\"src/main/kotlin/a/x\"]") {
		t.Errorf("want src/main/kotlin/a/x in the box src/main/kotlin/a/..., got\n%s", got)
	}
}

// viewpoint: external contract
// target: deps.DraftDiagram
// A draft stays in the user's repository, so its note points at the examples by design pattern,
// for whoever tidies it later.
func TestADraftPointsAtTheExamplesByPattern(t *testing.T) {
	const guide = "https://github.com/rerorets/hadome/blob/main/docs/patterns.md"
	a := &facts.Analysis{}
	for name, got := range map[string]string{"dependencies": deps.DraftDiagram(a, 2), "calls": deps.DraftCalls(a, 2)} {
		if note, _, _ := strings.Cut(got, "```"); !strings.Contains(note, guide) {
			t.Errorf("want the %s draft's note to point at %s, got\n%s", name, guide, got)
		}
	}
}
