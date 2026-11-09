package sql

import (
	"regexp"
	"sort"
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// Framework is one way of writing tests in SQL. Adding a way is adding one implementation to
// frameworks.
type Framework interface {
	Name() string
	// Tests returns the tests of a test file whose statements read as trees.
	Tests(d Dialect, trees []*Node, src []byte) []TestDef
}

// TestDef is one test written in SQL: the statements it runs, and what it asserts, skips and fakes.
type TestDef struct {
	Name  string
	Line  int
	Nodes []*Node
	// own reports whether a called name is the framework's own (an assertion, a fake), which is
	// neither a call of product code nor unresolved.
	own func(name string) bool
	// asserts reports whether a called name that is no product object is one of the framework's
	// assertions. A product function named like one (is_active) stays a call.
	asserts func(name string) bool
	Asserts int
	Skip    facts.Skip
	Mocks   []facts.Hazard
}

var frameworks = []Framework{tapFramework{}, tsqltFramework{}, utplsqlFramework{}}

// sqlTests returns the tests of a test .sql file, with what their statements call resolved against
// the file's database.
func sqlTests(f *sqlFile, cat *catalog) []facts.Test {
	var out []facts.Test
	for _, fw := range frameworks {
		for _, td := range fw.Tests(f.dialect, f.trees, f.src) {
			out = append(out, testFacts(f, td))
		}
	}
	return out
}

func testFacts(f *sqlFile, td TestDef) facts.Test {
	d := f.dialect.(tableDialect)
	t := facts.Test{Name: td.Name, Path: f.path, Line: td.Line, Asserts: td.Asserts, Skip: td.Skip}
	for _, m := range td.Mocks {
		m.Path = f.path
		t.Hazards = append(t.Hazards, m)
	}
	e := &effect{calls: map[string]*callCount{}}
	fixtures := fixtures(f, d, td.Nodes)
	var tokens []string
	for _, n := range td.Nodes {
		for _, ref := range d.References(n) {
			if key, ok := f.db.defKey(d, ref.Name); !ok || !fixtures[key] {
				t.Asserts += refFact(f, d, td, ref, e)
			}
		}
		e.hazards = append(e.hazards, hazardsIn(d, n, facts.Text{Path: f.path, Line: n.Line})...)
		for _, tk := range n.Tokens() {
			tokens = append(tokens, tk.Text)
		}
	}
	merge(&t, e)
	t.Body = facts.Fingerprint(tokens)
	return t
}

// fixtures returns the keys of the objects a test's statements create: its setup, whose names are
// not product code.
func fixtures(f *sqlFile, d tableDialect, nodes []*Node) map[string]bool {
	out := map[string]bool{}
	for _, n := range nodes {
		for _, def := range d.Definitions(n) {
			if key, ok := f.db.defKey(d, def.Name); ok && def.Effect == EffectCreate {
				out[key] = true
			}
		}
	}
	return out
}

// refFact adds what one name of a test written in SQL is: a product object, or the framework's
// assertion (it returns 1 then), or the framework's own function, or unresolved.
func refFact(f *sqlFile, d tableDialect, td TestDef, ref Reference, e *effect) int {
	o, out := f.db.resolve(d, ref)
	name := strings.ToLower(d.key(ref.Name))
	switch {
	case out == found:
		e.use(f.db, o, ref, 1)
	case ref.Use == UseCall && td.asserts != nil && td.asserts(name):
		return 1
	case ref.Use == UseCall && td.own(name):
	case out == unresolved:
		e.unresolved++
	}
	return 0
}

// tapFramework is pgTAP and myTAP: a file that calls plan(…) or no_plan() is one test.
type tapFramework struct{}

func (tapFramework) Name() string { return "pgTAP" }

var tapAsserts = regexp.MustCompile(`^(ok|is|isnt|matches|imatches|doesnt_match|doesnt_imatch|alike|ialike|unalike|unialike|` +
	`cmp_ok|pass|fail|isa_ok|row_eq|fk_ok|can|cant|throws_\w+|lives_ok|performs_\w+|results_\w+|set_\w+|bag_\w+|` +
	`has_\w+|hasnt_\w+|col_\w+|is_\w+|isnt_\w+|function_\w+|trigger_is|index_is_\w+|policy_\w+|schema_\w+|` +
	`tables_are|views_are|functions_are|sequences_are|columns_are|indexes_are|roles_are|users_are|enums_are|` +
	`enum_has_labels|domain_type_is|cast_context_is|language_\w+|volatility_is|rule_is_\w+|throws_ok|` +
	`\w+_are|\w+_is|\w+_isnt|\w+_ok|\w+_eq|\w+_ne)$`)

var tapOwn = regexp.MustCompile(`^(plan|no_plan|finish|diag|skip|todo|todo_start|todo_end|in_todo|runtests|` +
	`num_failed|os_name|pgtap_version|collect_tap|tap\.\w+)$`)

func (tapFramework) Tests(d Dialect, trees []*Node, src []byte) []TestDef {
	if d.Name() != "postgresql" && d.Name() != "mysql" {
		return nil
	}
	td := tableDialectOf(d)
	planned := false
	var skip facts.Skip
	for _, t := range trees {
		for _, r := range td.References(t) {
			if r.Use != UseCall {
				continue
			}
			name := strings.ToLower(lastIdent(r.Name))
			switch {
			case name == "plan" || name == "no_plan":
				planned = true
			case name == "skip":
				reason := ""
				if len(r.Args) > 0 {
					reason = strings.Trim(r.Args[0], "'\"")
				}
				if strings.Trim(reason, "0123456789") == "" {
					reason = "" // skip(count) gives no reason
				}
				skip = facts.Skip{Skipped: true, Reason: reason, Line: r.Line}
			}
		}
	}
	if !planned {
		return nil
	}
	// pgTAP's functions are called unqualified, or in the schema pgTAP is installed in.
	unqualified := func(name string) string {
		if i := strings.LastIndex(name, "."); i >= 0 {
			return name[i+1:]
		}
		return name
	}
	own := func(name string) bool { return tapOwn.MatchString(unqualified(name)) }
	asserts := func(name string) bool { return tapAsserts.MatchString(unqualified(name)) }
	return []TestDef{{Name: "plan", Line: 1, Nodes: trees, own: own, asserts: asserts, Skip: skip}}
}

func lastIdent(n QualifiedName) string {
	if len(n) == 0 {
		return ""
	}
	return n[len(n)-1].Text
}

func tableDialectOf(d Dialect) tableDialect { return d.(tableDialect) }

// tsqltFramework is tSQLt: procedures whose names start with test, in a schema made a test class by
// tSQLt.NewTestClass.
type tsqltFramework struct{}

func (tsqltFramework) Name() string { return "tSQLt" }

var newTestClass = regexp.MustCompile(`(?i)tSQLt\.NewTestClass\s*@?\w*\s*=?\s*N?'([^']+)'`)

func (tsqltFramework) Tests(d Dialect, trees []*Node, src []byte) []TestDef {
	if d.Name() != "tsql" {
		return nil
	}
	td := tableDialectOf(d)
	classes := map[string]bool{}
	for _, m := range newTestClass.FindAllStringSubmatch(string(src), -1) {
		classes[strings.ToLower(m[1])] = true
	}
	var out []TestDef
	for _, t := range trees {
		for _, stmt := range td.statements(t) {
			for _, def := range td.definitions(stmt) {
				if isTSQLtTest(def, classes) {
					out = append(out, tsqltTest(td, stmt, def))
				}
			}
		}
	}
	return out
}

// isTSQLtTest reports whether a definition creates a procedure whose name starts with test in a
// test class.
func isTSQLtTest(def Definition, classes map[string]bool) bool {
	return def.Kind == KindProcedure && def.Effect == EffectCreate && len(def.Name) == 2 &&
		classes[strings.ToLower(def.Name[0].Text)] && strings.HasPrefix(strings.ToLower(def.Name[1].Text), "test")
}

var tsqltFakes = map[string]bool{"tsqlt.faketable": true, "tsqlt.spyprocedure": true, "tsqlt.fakefunction": true}

// tsqltTest is the test a tSQLt test procedure is, with the fakes it makes as mocks.
func tsqltTest(td tableDialect, stmt *Node, def Definition) TestDef {
	t := TestDef{Name: def.Name[0].Text + "." + def.Name[1].Text, Line: def.Line, Nodes: []*Node{stmt},
		own: func(name string) bool { return strings.HasPrefix(name, "tsqlt.") },
		asserts: func(name string) bool {
			return strings.HasPrefix(name, "tsqlt.assert") || name == "tsqlt.fail" || strings.HasPrefix(name, "tsqlt.expectexception")
		}}
	for _, r := range td.References(stmt) {
		if name := strings.ToLower(td.key(r.Name)); tsqltFakes[name] {
			t.Mocks = append(t.Mocks, facts.Hazard{Category: facts.HazardMock, Line: r.Line, Evidence: name})
		}
	}
	return t
}

// utplsqlFramework is utPLSQL: procedures annotated --%test in a package annotated --%suite. Their
// bodies are in the package body.
type utplsqlFramework struct{}

func (utplsqlFramework) Name() string { return "utPLSQL" }

var (
	suiteNote    = regexp.MustCompile(`(?im)^\s*--\s*%suite\b`)
	testNote     = regexp.MustCompile(`(?i)^\s*--\s*%test\b`)
	disabledNote = regexp.MustCompile(`(?i)^\s*--\s*%disabled(\((.*)\))?`)
)

func (utplsqlFramework) Tests(d Dialect, trees []*Node, src []byte) []TestDef {
	if d.Name() != "oracle" || !suiteNote.Match(src) {
		return nil
	}
	suite := readSuite(tableDialectOf(d), trees, strings.Split(string(src), "\n"))
	var names []string
	for k := range suite.tests {
		names = append(names, k)
	}
	sort.Strings(names)
	var out []TestDef
	for _, k := range names {
		note := suite.tests[k]
		t := TestDef{Name: k, Line: note.line,
			own: func(name string) bool { return strings.HasPrefix(name, "ut.") || strings.HasPrefix(name, "ut3.") },
			asserts: func(name string) bool {
				return strings.HasPrefix(name, "ut.expect") || strings.HasPrefix(name, "ut3.ut.expect")
			}}
		if note.disabled != nil {
			t.Skip = *note.disabled
		}
		if b := suite.bodies[k]; b != nil {
			t.Nodes = []*Node{b}
		}
		out = append(out, t)
	}
	return out
}

// utSuite is what a utPLSQL file declares: the annotated procedures of its package specifications
// and the procedure bodies of its package bodies, by package.procedure.
type utSuite struct {
	tests  map[string]utNote
	bodies map[string]*Node
}

type utNote struct {
	line     int
	disabled *facts.Skip
}

func readSuite(td tableDialect, trees []*Node, lines []string) utSuite {
	suite := utSuite{tests: map[string]utNote{}, bodies: map[string]*Node{}}
	for _, t := range trees {
		for _, stmt := range td.statements(t) {
			defs := td.definitions(stmt)
			if len(defs) == 0 {
				continue
			}
			pkg := strings.ToUpper(lastIdent(defs[0].Name))
			switch defs[0].Kind {
			case KindPackage:
				for _, def := range defs[1:] {
					if skip, ok := annotated(lines, def.Line); ok {
						suite.tests[pkg+"."+strings.ToUpper(lastIdent(def.Name))] = utNote{line: def.Line, disabled: skip}
					}
				}
			case KindPackageBody:
				for _, b := range stmt.All(func(m *Node) bool { return m.Rule == "procedure_body" }) {
					if id := b.Child("identifier"); id != nil {
						suite.bodies[pkg+"."+strings.ToUpper(td.nameOf(id).last())] = b
					}
				}
			}
		}
	}
	return suite
}

// annotated reads the utPLSQL annotations right above a line: whether it is a --%test, and the
// skip a --%disabled gives.
func annotated(lines []string, line int) (*facts.Skip, bool) {
	isTest := false
	var skip *facts.Skip
	for i := line - 2; i >= 0; i-- {
		l := strings.TrimSpace(lines[i])
		if !strings.HasPrefix(l, "--") {
			break
		}
		if testNote.MatchString(l) {
			isTest = true
		}
		if m := disabledNote.FindStringSubmatch(l); m != nil {
			skip = &facts.Skip{Skipped: true, Reason: strings.TrimSpace(m[2]), Line: i + 1}
		}
	}
	return skip, isTest
}

func (q QualifiedName) last() string { return lastIdent(q) }
