//go:build cgo

package treesitter

import (
	"slices"
	"strings"

	sitter "github.com/tree-sitter/go-tree-sitter"
)

// childField returns the child of n in field name, or nil.
func childField(n *sitter.Node, name string) *sitter.Node {
	if n == nil {
		return nil
	}
	return n.ChildByFieldName(name)
}

// kindOf returns n's kind, or "" for nil.
func kindOf(n *sitter.Node) string {
	if n == nil {
		return ""
	}
	return n.Kind()
}

// firstArg returns the first argument of an argument list, or nil.
func firstArg(list *sitter.Node) *sitter.Node {
	if args := arguments(list); len(args) > 0 {
		return args[0]
	}
	return nil
}

// lastName returns the last name of a callee: b of a.b, c of a::b::c.
func lastName(callee string) string {
	callee = strings.TrimSuffix(callee, "!")
	i := strings.LastIndexAny(callee, ".:")
	return callee[i+1:]
}

func sinks(dynamic []string, member []string, literal []string) []sqlSink {
	var out []sqlSink
	for _, n := range dynamic {
		out = append(out, sqlSink{name: n, dynamic: true})
	}
	for _, n := range member {
		out = append(out, sqlSink{name: n, dynamic: true, member: true})
	}
	for _, n := range literal {
		out = append(out, sqlSink{name: n})
	}
	return out
}

var tsText = &textGrammar{
	literals: set("string", "template_string"),
	concat:   set("binary_expression"),
	holes:    set("template_substitution"),
	escapes:  set("escape_sequence"),
	idents:   set("identifier"),
	notRead:  set("name", "key", "label"),
	declare: func(f *file, n *sitter.Node, kind string) (*sitter.Node, *sitter.Node) {
		if kind != "variable_declarator" || kindOf(childField(n, "name")) != "identifier" {
			return nil, nil
		}
		return n.ChildByFieldName("name"), n.ChildByFieldName("value")
	},
	call: func(f *file, n *sitter.Node, kind string) (string, string, *sitter.Node, bool) {
		if kind != "call_expression" {
			return "", "", nil, false
		}
		fn, args := n.ChildByFieldName("function"), n.ChildByFieldName("arguments")
		if fn == nil || args == nil {
			return "", "", nil, false
		}
		first := firstArg(args)
		if args.Kind() == "template_string" { // a tagged template: sql`…`
			first = args
		}
		callee := strings.ReplaceAll(f.text(fn), "?.", ".")
		return callee, lastName(callee), first, true
	},
	consts: func(f *file) map[string]*sitter.Node {
		out := map[string]*sitter.Node{}
		for _, top := range named(f.root) {
			if top.Kind() == "export_statement" {
				if d := top.ChildByFieldName("declaration"); d != nil {
					top = d
				}
			}
			if top.Kind() != "lexical_declaration" || f.field(top, "kind") != "const" {
				continue
			}
			for _, d := range named(top) {
				if d.Kind() == "variable_declarator" && kindOf(childField(d, "name")) == "identifier" && d.ChildByFieldName("value") != nil {
					out[f.field(d, "name")] = d.ChildByFieldName("value")
				}
			}
		}
		return out
	},
	// pg, mysql2, Prisma, Sequelize; better-sqlite3's prepare and exec, knex.raw and the tagged
	// templates of postgres and slonik.
	sinks: sinks([]string{"$queryRaw", "$executeRaw", "$queryRawUnsafe", "$executeRawUnsafe"},
		[]string{"query", "execute"}, []string{"prepare", "exec", "raw", "sql"}),
}

var pyText = &textGrammar{
	literals: set("string"),
	adjacent: set("concatenated_string"),
	concat:   set("binary_operator"),
	holes:    set("interpolation"),
	escapes:  set("escape_sequence"),
	raw: func(f *file, n *sitter.Node) bool {
		start := n.Child(0)
		return start != nil && strings.ContainsAny(f.text(start), "rR")
	},
	idents:  set("identifier"),
	notRead: set("attribute", "name", "left"),
	declare: func(f *file, n *sitter.Node, kind string) (*sitter.Node, *sitter.Node) {
		if kind != "assignment" || kindOf(childField(n, "left")) != "identifier" {
			return nil, nil
		}
		return n.ChildByFieldName("left"), n.ChildByFieldName("right")
	},
	call: func(f *file, n *sitter.Node, kind string) (string, string, *sitter.Node, bool) {
		if kind != "call" || n.ChildByFieldName("function") == nil {
			return "", "", nil, false
		}
		var first *sitter.Node
		if args := positional(n.ChildByFieldName("arguments")); len(args) > 0 {
			first = args[0]
		}
		callee := f.field(n, "function")
		return callee, lastName(callee), first, true
	},
	consts: func(f *file) map[string]*sitter.Node {
		out := map[string]*sitter.Node{}
		for _, top := range named(f.root) {
			if top.Kind() != "expression_statement" || top.NamedChildCount() != 1 {
				continue
			}
			a := top.NamedChild(0)
			if a.Kind() == "assignment" && kindOf(childField(a, "left")) == "identifier" && a.ChildByFieldName("right") != nil {
				out[f.field(a, "left")] = a.ChildByFieldName("right")
			}
		}
		return out
	},
	// DB-API, SQLAlchemy's text and exec_driver_sql, asyncpg, and the query of the BigQuery and
	// Snowflake clients (which shares its name with SQLAlchemy's session.query(Model)).
	sinks: sinks([]string{"executemany", "executescript", "exec_driver_sql"}, []string{"execute"},
		[]string{"text", "fetch", "fetchrow", "fetchval", "query"}),
}

var rustText = &textGrammar{
	literals: set("string_literal", "raw_string_literal"),
	escapes:  set("escape_sequence"),
	raw:      func(f *file, n *sitter.Node) bool { return n.Kind() == "raw_string_literal" },
	idents:   set("identifier"),
	notRead:  set("name", "pattern", "field"),
	declare: func(f *file, n *sitter.Node, kind string) (*sitter.Node, *sitter.Node) {
		if kind != "let_declaration" || kindOf(childField(n, "pattern")) != "identifier" {
			return nil, nil
		}
		return n.ChildByFieldName("pattern"), n.ChildByFieldName("value")
	},
	call: func(f *file, n *sitter.Node, kind string) (string, string, *sitter.Node, bool) {
		switch kind {
		case "call_expression":
			fn := n.ChildByFieldName("function")
			if fn == nil {
				return "", "", nil, false
			}
			if fn.Kind() == "generic_function" { // query_as::<_, Row>(…)
				fn = fn.ChildByFieldName("function")
			}
			callee := f.text(fn)
			return callee, lastName(callee), firstArg(n.ChildByFieldName("arguments")), true
		case "macro_invocation":
			callee := f.field(n, "macro") + "!"
			var first *sitter.Node
			for i := uint(0); i < n.NamedChildCount(); i++ {
				if tt := n.NamedChild(i); tt.Kind() == "token_tree" {
					first = firstArg(tt)
				}
			}
			return callee, lastName(callee) + "!", first, true
		}
		return "", "", nil, false
	},
	consts: func(f *file) map[string]*sitter.Node {
		out := map[string]*sitter.Node{}
		walk(f.root, func(n *sitter.Node) bool {
			if n.Kind() == "function_item" {
				return false
			}
			if (n.Kind() == "const_item" || n.Kind() == "static_item") && n.ChildByFieldName("value") != nil {
				out[f.field(n, "name")] = n.ChildByFieldName("value")
			}
			return true
		})
		return out
	},
	sinks: sinks([]string{"sqlx::query", "sqlx::query_as", "sqlx::query_scalar"}, nil,
		[]string{"query", "query_as", "query_scalar", "query!", "query_as!", "query_scalar!"}),
}

// jvmSinks are JDBC, Spring's JdbcTemplate and JPA, for Java and Kotlin alike.
var jvmSinks = sinks([]string{"prepareStatement", "prepareCall", "executeQuery", "executeUpdate", "queryForObject",
	"queryForList", "queryForMap", "batchUpdate", "createNativeQuery"}, nil, []string{"execute", "addBatch", "query", "update"})

var javaText = &textGrammar{
	literals: set("string_literal"),
	concat:   set("binary_expression"),
	escapes:  set("escape_sequence"),
	idents:   set("identifier"),
	notRead:  set("name", "field"),
	declare: func(f *file, n *sitter.Node, kind string) (*sitter.Node, *sitter.Node) {
		if kind != "variable_declarator" {
			return nil, nil
		}
		return n.ChildByFieldName("name"), n.ChildByFieldName("value")
	},
	call: func(f *file, n *sitter.Node, kind string) (string, string, *sitter.Node, bool) {
		if kind != "method_invocation" {
			return "", "", nil, false
		}
		name := f.field(n, "name")
		callee := name
		if obj := n.ChildByFieldName("object"); obj != nil {
			callee = f.text(obj) + "." + name
		}
		return callee, name, firstArg(n.ChildByFieldName("arguments")), true
	},
	consts: func(f *file) map[string]*sitter.Node {
		out := map[string]*sitter.Node{}
		walk(f.root, func(n *sitter.Node) bool {
			if n.Kind() == "method_declaration" || n.Kind() == "constructor_declaration" {
				return false
			}
			if n.Kind() != "field_declaration" && n.Kind() != "constant_declaration" {
				return true
			}
			mods := ""
			for _, c := range named(n) {
				if c.Kind() == "modifiers" {
					mods = " " + f.text(c) + " "
				}
			}
			if n.Kind() == "field_declaration" && (!strings.Contains(mods, " static ") || !strings.Contains(mods, " final ")) {
				return false
			}
			for _, d := range named(n) {
				if d.Kind() == "variable_declarator" && d.ChildByFieldName("value") != nil {
					out[f.field(d, "name")] = d.ChildByFieldName("value")
				}
			}
			return false
		})
		return out
	},
	sinks: jvmSinks,
}

var ktText = &textGrammar{
	literals:    set("string_literal", "multiline_string_literal"),
	concat:      set("binary_expression", "additive_expression"),
	holes:       set("interpolation"),
	escapes:     set("escape_sequence"),
	dollarHoles: true,
	idents:      set("identifier", "simple_identifier"),
	notReadIn: func(n, p *sitter.Node) bool {
		switch p.Kind() {
		case "navigation_expression": // a.b: b is a member
			return p.NamedChild(0).Id() != n.Id()
		case "variable_declaration", "value_argument_label", "user_type", "type_identifier":
			return true
		case "value_argument": // name = value
			return p.NamedChildCount() > 1 && p.NamedChild(0).Id() == n.Id()
		}
		return false
	},
	declare: func(f *file, n *sitter.Node, kind string) (*sitter.Node, *sitter.Node) {
		if kind != "property_declaration" {
			return nil, nil
		}
		return ktPropertyValue(n)
	},
	call: func(f *file, n *sitter.Node, kind string) (string, string, *sitter.Node, bool) {
		if kind != "call_expression" || n.NamedChildCount() < 2 {
			return "", "", nil, false
		}
		var args *sitter.Node
		for _, c := range named(n) {
			if c.Kind() == "value_arguments" {
				args = c
			}
		}
		if args == nil {
			return "", "", nil, false
		}
		var first *sitter.Node
		if list := arguments(args); len(list) > 0 {
			first = ktArgumentValue(list[0])
		}
		callee := strings.ReplaceAll(f.text(n.NamedChild(0)), "?.", ".")
		return callee, lastName(callee), first, true
	},
	consts: func(f *file) map[string]*sitter.Node {
		out := map[string]*sitter.Node{}
		walk(f.root, func(n *sitter.Node) bool {
			switch n.Kind() {
			case "function_declaration", "class_declaration":
				return n.Kind() == "class_declaration" // a class's companion object holds constants; its members do not
			case "property_declaration":
				if !ktConstScope(n) || !strings.HasPrefix(strings.TrimSpace(ktAfterModifiers(f, n)), "val") {
					return false
				}
				if name, value := ktPropertyValue(n); name != nil && value != nil {
					out[f.text(name)] = value
				}
				return false
			}
			return true
		})
		return out
	},
	sinks: jvmSinks,
}

// ktPropertyValue returns the name and the initial value of a Kotlin property declaration.
func ktPropertyValue(n *sitter.Node) (name, value *sitter.Node) {
	kids := named(n)
	i := slices.IndexFunc(kids, func(c *sitter.Node) bool { return c.Kind() == "variable_declaration" })
	if i < 0 {
		return nil, nil
	}
	decl := kids[i]
	if decl.NamedChildCount() == 0 {
		return nil, nil
	}
	name = decl.NamedChild(0)
	for _, c := range kids[i+1:] {
		if !strings.Contains(c.Kind(), "comment") && c.Kind() != "property_delegate" && c.Kind() != "getter" && c.Kind() != "setter" {
			value = c
		}
	}
	return name, value
}

// ktConstScope reports whether a property is declared where it is one value for the whole file: at
// the top level, or in an object or a companion object.
func ktConstScope(n *sitter.Node) bool {
	p := n.Parent()
	if p.Kind() == "source_file" {
		return true
	}
	if p.Kind() != "class_body" || p.Parent() == nil {
		return false
	}
	k := p.Parent().Kind()
	return k == "companion_object" || k == "object_declaration"
}

// ktAfterModifiers returns the text of a property declaration after its modifiers: val … or var ….
func ktAfterModifiers(f *file, n *sitter.Node) string {
	for i := uint(0); i < n.ChildCount(); i++ {
		if c := n.Child(i); c.Kind() != "modifiers" && !strings.Contains(c.Kind(), "comment") {
			return string(f.src[c.StartByte():n.EndByte()])
		}
	}
	return ""
}
