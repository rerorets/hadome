package ansible

import (
	"path"
	"regexp"
	"sort"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/token"

	"github.com/rerorets/hadome/internal/facts"
)

var (
	assertTasks = modules("assert")
	sleepTasks  = modules("pause")
)

// projectRole is how a scenario inside a role names that role: Molecule sets
// MOLECULE_PROJECT_DIRECTORY to the role's directory, so its base name is the role.
var projectRole = regexp.MustCompile(`^\{\{\s*lookup\(\s*['"](ansible\.builtin\.)?env['"]\s*,\s*['"]MOLECULE_PROJECT_DIRECTORY['"]\s*\)\s*\|\s*basename\s*\}\}$`)

// enclosingRole is the role a scenario directory is inside (roles/web/molecule/default -> web), or "".
func enclosingRole(scenario string) string {
	elems := strings.Split(scenario, "/")
	for i := 0; i+2 < len(elems); i++ {
		if elems[i] == "roles" && elems[i+2] == "molecule" {
			return elems[i+1]
		}
	}
	return ""
}

// test reads a scenario's verify playbook as one test named after the scenario. Its asserts are its
// assert tasks and the tasks with failed_when; what it calls are the repository's roles the scenario's
// converge playbook applies, a role being a unit. A role named with {{ }} is a call hadome cannot
// identify, except Molecule's name for the role a scenario is inside.
func test(verify, converge parsed, roles map[string]bool) facts.Test {
	t := facts.Test{Name: path.Base(verify.place.dir), Path: verify.path, Line: 1, Body: fingerprint(verify.src)}
	for _, doc := range verify.body {
		if plays := items(doc); len(plays) > 0 {
			t.Line = line(plays[0])
			break
		}
	}
	t.Declared = facts.Declarations(docLines(verify.comments, t.Line))
	for _, doc := range verify.body {
		for _, play := range items(doc) {
			for _, section := range []string{"pre_tasks", "tasks", "post_tasks", "handlers"} {
				walkTasks(value(play, section), func(task ast.Node) { check(&t, verify.path, task) })
			}
		}
	}
	calls := map[string]int{}
	for _, r := range roleRefs(converge) {
		if own := enclosingRole(verify.place.dir); own != "" && projectRole.MatchString(strings.TrimSpace(r.name)) {
			r.name = own
		}
		switch {
		case template.MatchString(r.name):
			t.Unresolved++
		case roles[r.name]:
			calls[r.name]++
		}
	}
	for target, n := range calls {
		t.Calls = append(t.Calls, facts.Call{Target: target, Times: n})
	}
	sort.Slice(t.Calls, func(i, j int) bool { return t.Calls[i].Target < t.Calls[j].Target })
	return t
}

// check counts a task that asserts once, and notes a pause as a sleep.
func check(t *facts.Test, p string, task ast.Node) {
	asserts := false
	for _, kv := range entries(task) {
		key := keyOf(kv)
		switch {
		case assertTasks[key] || key == "failed_when":
			asserts = true
		case sleepTasks[key]:
			t.Hazards = append(t.Hazards, facts.Hazard{Category: facts.HazardSleep, Path: p, Line: line(kv.Key),
				Evidence: strings.Join(strings.Fields(key+": "+kv.Value.String()), " ")})
		}
	}
	if asserts {
		t.Asserts++
	}
}

// fingerprint is the playbook's tokens: its comments and layout are left out.
func fingerprint(src []byte) string {
	var words []string
	for _, tk := range lexer.Tokenize(string(src)) {
		if tk.Type != token.CommentType {
			words = append(words, tk.Value)
		}
	}
	return facts.Fingerprint(words)
}

// docLines returns the comments on the lines right before line: the test's doc comment. A blank line
// ends it.
func docLines(cs []facts.Comment, line int) []string {
	var lines []string
	want := line - 1
	for i := len(cs) - 1; i >= 0; i-- {
		c := cs[i]
		if c.Line > want {
			continue
		}
		if c.Line < want {
			break
		}
		lines = append([]string{c.Text}, lines...)
		want--
	}
	return lines
}
