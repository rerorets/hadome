// Package ansible is the Ansible language adapter. It reads YAML in pure Go, so it reads in builds
// without cgo too. Ansible has no functions or classes: a role is a unit, the variables of its
// defaults are its symbols, and each Molecule scenario's verify playbook is a test.
//
// Design: docs/design/adapters-iac.md
package ansible

import (
	"io/fs"
	"path"
	"regexp"
	"slices"
	"strings"

	"github.com/goccy/go-yaml/ast"
	"github.com/goccy/go-yaml/lexer"
	"github.com/goccy/go-yaml/parser"
	"github.com/goccy/go-yaml/token"

	"github.com/rerorets/hadome/internal/adapter/reuse"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
)

// Adapter is the Ansible adapter.
type Adapter struct{}

const name = "ansible"

// Name is the language's name in configuration.
func (Adapter) Name() string { return name }

// Claims reports whether the file is in Ansible's layout. YAML is also the language of CI settings
// and of .hadome.yml, so only these places are Ansible's; files.ansible adds more (ClaimsConfigured).
func (Adapter) Claims(p string) bool {
	_, ok := locate(p)
	return ok
}

// ClaimsConfigured reports whether the file is in Ansible's layout or is a playbook files.ansible
// names.
func (a Adapter) ClaimsConfigured(c config.Analysis, p string) bool {
	return a.Claims(p) || config.MatchesAny(c.AnsibleFiles, p)
}

// kind is what a file is in Ansible's layout.
type kind int

const (
	roleFile kind = iota // a file of roles/<role>/{tasks,handlers,defaults,vars,meta}
	playbook
	scenarioFile // a file of molecule/<scenario>/
)

// place is where a file is in Ansible's layout.
type place struct {
	kind kind
	// dir is the role's directory (roles/web), the scenario's (molecule/default), or the playbook's.
	dir string
	// role is the role's name, and part the directory of the role the file is in (tasks, meta, …).
	role, part string
}

var roleParts = []string{"tasks", "handlers", "defaults", "vars", "meta"}

// locate finds a YAML file's place in the layout. roles/, playbooks/ and molecule/ may be at any
// depth, so that an Ansible tree inside a larger repository (ansible/roles/web) is read too.
func locate(p string) (place, bool) {
	if !strings.HasSuffix(p, ".yml") && !strings.HasSuffix(p, ".yaml") {
		return place{}, false
	}
	if p == "site.yml" || p == "playbook.yml" {
		return place{kind: playbook, dir: "."}, true
	}
	elems := strings.Split(p, "/")
	last := len(elems) - 1
	for i := range last { // molecule first: a role's scenarios are tests, not part of the role
		if elems[i] == "molecule" && i+2 <= last {
			return place{kind: scenarioFile, dir: strings.Join(elems[:i+2], "/")}, true
		}
	}
	for i := range last {
		if elems[i] == "roles" && i+3 <= last && slices.Contains(roleParts, elems[i+2]) {
			return place{kind: roleFile, dir: strings.Join(elems[:i+2], "/"), role: elems[i+1], part: elems[i+2]}, true
		}
	}
	for i := range last {
		if elems[i] == "playbooks" {
			return place{kind: playbook, dir: path.Dir(p)}, true
		}
	}
	return place{}, false
}

// Analyze reads the Ansible files of the version. A file that is not YAML is listed as unread and
// yields nothing else, as in the Go adapter. Files matching files.tests are not made tests: Molecule's
// layout decides what a test is.
func (Adapter) Analyze(r *reuse.Run, c config.Analysis, paths []string) (*facts.Analysis, error) {
	v := engine.Version{Label: r.Label(), FS: r.FS()}
	a := &facts.Analysis{}
	tree := tree{roles: map[string]bool{}, specs: map[string]map[string]bool{}, converges: map[string]parsed{}}
	for _, p := range paths {
		src, err := fs.ReadFile(v.FS, p)
		if err != nil {
			return nil, err
		}
		pf, ok := parse(p, src, c)
		a.Files = append(a.Files, pf.file)
		tree.add(pf, ok)
	}
	for _, pf := range tree.files {
		a.Comments = append(a.Comments, pf.comments...)
		switch {
		case pf.place.kind == scenarioFile:
			if isMain(pf.path, "verify") {
				a.Tests = append(a.Tests, test(pf, tree.converges[pf.place.dir], tree.roles))
			}
			continue
		case pf.place.kind == roleFile && pf.place.part == "defaults" && isMain(pf.path, "main") && pf.file.Role == facts.RoleSource:
			a.Symbols = append(a.Symbols, symbols(pf, tree.specs[pf.place.role])...)
		}
		for _, r := range roleRefs(pf) {
			a.Imports = append(a.Imports, importOf(pf, r, tree.roles, c.IO))
		}
	}
	return a, nil
}

// tree is what the files of a version tell about one another.
type tree struct {
	files []parsed
	roles map[string]bool            // the repository's roles
	specs map[string]map[string]bool // role -> the variables its argument_specs describes
	// converges are the scenarios' converge playbooks, by scenario directory.
	converges map[string]parsed
}

// add records a file; read is false when it could not be read, and it then only names its role.
func (t *tree) add(pf parsed, read bool) {
	if pf.place.kind == roleFile {
		t.roles[pf.place.role] = true
	}
	if !read {
		return
	}
	t.files = append(t.files, pf)
	switch {
	case pf.place.kind == roleFile && pf.place.part == "meta" && isMain(pf.path, "argument_specs"):
		if t.specs[pf.place.role] == nil {
			t.specs[pf.place.role] = map[string]bool{}
		}
		described(pf.body, t.specs[pf.place.role])
	case pf.place.kind == scenarioFile && isMain(pf.path, "converge"):
		t.converges[pf.place.dir] = pf
	}
}

// isMain reports whether the file is <name>.yml or <name>.yaml.
func isMain(p, name string) bool {
	base := path.Base(p)
	return base == name+".yml" || base == name+".yaml"
}

// parsed is one file read as YAML.
type parsed struct {
	path     string
	place    place
	src      []byte
	body     []ast.Node // each document's body
	file     facts.File
	comments []facts.Comment
}

// parse reads one file. It reports false, with the file listed as unread, when it is not YAML.
func parse(p string, src []byte, c config.Analysis) (parsed, bool) {
	pl, ok := locate(p)
	if !ok { // claimed through files.ansible
		pl = place{kind: playbook, dir: path.Dir(p)}
	}
	lines := facts.CountLines(src)
	f, err := parser.ParseBytes(src, 0)
	if err != nil {
		return parsed{place: pl, file: facts.File{Path: p, Language: name, Role: facts.RoleOther, Lines: lines, Unread: true}}, false
	}
	pf := parsed{path: p, place: pl, src: src, comments: lexComments(p, src)}
	for _, d := range f.Docs {
		if d.Body != nil {
			pf.body = append(pf.body, d.Body)
		}
	}
	unit := pl.dir
	if pl.kind == roleFile {
		unit = pl.role
	}
	pf.file = facts.File{Path: p, Language: name, Lines: lines, Unit: unit, UnitPath: pl.dir}
	switch {
	case pl.kind == scenarioFile:
		pf.file.Role = facts.RoleTest
	case config.MatchesAny(c.GeneratedFiles, p) || generated(pf.comments):
		pf.file.Role = facts.RoleGenerated
	default:
		pf.file.Role = facts.RoleSource
	}
	return pf, true
}

// symbols lists the variables of a role's defaults, the values a playbook may set: exported unless
// the name starts with _, which by convention keeps it to the role, and documented when the role's
// argument_specs describes it.
func symbols(pf parsed, described map[string]bool) []facts.Symbol {
	var out []facts.Symbol
	for _, body := range pf.body {
		for _, kv := range entries(body) {
			key := keyOf(kv)
			if key == "" {
				continue
			}
			out = append(out, facts.Symbol{ID: pf.place.role + "." + key, Kind: facts.SymbolValue, Path: pf.path,
				Line: line(kv.Key), EndLine: lastLine(kv), Exported: !strings.HasPrefix(key, "_"), Doc: described[key]})
		}
	}
	return out
}

// described marks the variables a meta/argument_specs.yml describes, in any of its entry points.
func described(body []ast.Node, into map[string]bool) {
	for _, doc := range body {
		for _, entry := range entries(value(doc, "argument_specs")) {
			for _, opt := range entries(value(entry.Value, "options")) {
				if d := value(opt.Value, "description"); d != nil && strings.TrimSpace(text(d)) != "" {
					into[keyOf(opt)] = true
				}
			}
		}
	}
}

// roleRef is one place a file names a role.
type roleRef struct {
	name string
	line int
}

// roleRefs lists the roles a file uses: a role's meta/main.yml dependencies, a playbook's roles, and
// the include_role and import_role tasks of a role's tasks and handlers or of a playbook.
func roleRefs(pf parsed) []roleRef {
	var out []roleRef
	for _, doc := range pf.body {
		switch {
		case pf.place.kind == roleFile && pf.place.part == "meta":
			if isMain(pf.path, "main") {
				out = append(out, roleList(value(doc, "dependencies"))...)
			}
		case pf.place.kind == roleFile && (pf.place.part == "tasks" || pf.place.part == "handlers"):
			out = append(out, taskRoles(doc)...)
		case pf.place.kind == playbook || pf.place.kind == scenarioFile:
			out = append(out, playRoles(doc)...)
		}
	}
	return out
}

// playRoles lists the roles the plays of a playbook use, through roles: and through tasks.
func playRoles(doc ast.Node) []roleRef {
	var out []roleRef
	for _, play := range items(doc) {
		out = append(out, roleList(value(play, "roles"))...)
		for _, section := range []string{"pre_tasks", "tasks", "post_tasks", "handlers"} {
			out = append(out, taskRoles(value(play, section))...)
		}
	}
	return out
}

// roleList reads a list of roles, each a name or a mapping with role: or name:.
func roleList(n ast.Node) []roleRef {
	var out []roleRef
	for _, it := range items(n) {
		if s, ok := scalar(it); ok {
			out = append(out, roleRef{s, line(it)})
			continue
		}
		for _, k := range []string{"role", "name"} {
			if v := value(it, k); v != nil {
				out = append(out, roleRef{text(v), line(v)})
				break
			}
		}
	}
	return out
}

var roleTasks = modules("include_role", "import_role")

// taskRoles lists the roles a list of tasks includes or imports, inside blocks too.
func taskRoles(n ast.Node) []roleRef {
	var out []roleRef
	walkTasks(n, func(task ast.Node) {
		for _, kv := range entries(task) {
			if roleTasks[keyOf(kv)] {
				if v := value(kv.Value, "name"); v != nil {
					out = append(out, roleRef{text(v), line(v)})
				}
			}
		}
	})
	return out
}

// walkTasks calls visit for every task of a list, and for the tasks of every block, rescue and
// always section inside it.
func walkTasks(n ast.Node, visit func(ast.Node)) {
	for _, task := range items(n) {
		visit(task)
		for _, section := range []string{"block", "rescue", "always"} {
			walkTasks(value(task, section), visit)
		}
	}
}

// modules is the set of a module's names: short, and under ansible.builtin and ansible.legacy.
func modules(names ...string) map[string]bool {
	out := map[string]bool{}
	for _, n := range names {
		out[n], out["ansible.builtin."+n], out["ansible.legacy."+n] = true, true, true
	}
	return out
}

var template = regexp.MustCompile(`\{\{.*\}\}`)

// importOf turns a role a file names into an import. A role of the repository is internal and is its
// own unit; any other is named by its collection (community.general for community.general.ufw) or,
// for a standalone role, by its name. A name with {{ }} is decided only when the play runs.
func importOf(pf parsed, r roleRef, roles map[string]bool, io []string) facts.Import {
	im := facts.Import{Path: pf.path, Line: r.line, FromUnit: pf.file.Unit, Text: r.name}
	switch {
	case template.MatchString(r.name):
		im.Unknown = true
		return im
	case roles[r.name]:
		im.ToUnit, im.Internal = r.name, true
	default:
		im.ToUnit = r.name
		if parts := strings.Split(r.name, "."); len(parts) > 2 {
			im.ToUnit = parts[0] + "." + parts[1]
		}
	}
	im.IO = slices.ContainsFunc(io, func(e string) bool { return im.ToUnit == e || strings.HasPrefix(im.ToUnit, e+".") })
	return im
}

var generatedMark = regexp.MustCompile(`(?i)(@generated|code generated .* do not edit|auto-generated)`)

// generated reports whether one of the first comments marks the file as generated.
func generated(cs []facts.Comment) bool {
	for i, c := range cs {
		if i > 2 {
			break
		}
		if generatedMark.MatchString(c.Text) {
			return true
		}
	}
	return false
}

// directives are comments that instruct ansible-lint or yamllint rather than explain the code.
var directives = []string{"noqa", "yamllint"}

// lexComments lists the comments of a file, without the #.
func lexComments(p string, src []byte) []facts.Comment {
	var out []facts.Comment
	for _, tk := range lexer.Tokenize(string(src)) {
		if tk.Type != token.CommentType {
			continue
		}
		text := strings.TrimSpace(tk.Value)
		directive := false
		for _, d := range directives {
			directive = directive || strings.HasPrefix(text, d)
		}
		out = append(out, facts.Comment{Path: p, Line: tk.Position.Line, Text: text, Directive: directive})
	}
	return out
}
