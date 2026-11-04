// Package config reads, validates and compares hadome's configuration.
//
// One table, the specification, describes every key. The JSON Schema, `hadome explain` and the
// validation messages are all generated from it, so they cannot disagree.
//
// Design: docs/design/configuration.md
package config

import "strconv"

// Kind is the type of a value in the configuration.
type Kind int

// Kinds of values. Int and String are distinct: `schema: "1"` is an error, not a 1.
const (
	KindObject Kind = iota
	KindList
	KindString
	KindInt
	// KindAny accepts any value and passes it on untouched, for data hadome does not interpret.
	KindAny
)

// Direction says which change of a key makes rules more permissive than before.
type Direction int

// Directions.
const (
	// LooserNever means no change of the key loosens anything.
	LooserNever Direction = iota
	// LooserWhenRemoved means removing an entry that base had loosens the rules.
	LooserWhenRemoved
	// LooserWhenChanged means removing an entry, or changing one that keeps its identity, loosens the
	// rules: for entries whose effect hadome cannot judge, any change may be a loosening.
	LooserWhenChanged
	// LooserWhenHigher means a larger number loosens the rules, such as a size limit.
	LooserWhenHigher
	// LooserWhenAdded means adding an entry loosens the rules: the entries excuse something, such as
	// functions that count as assertions.
	LooserWhenAdded
)

// Value is one allowed value of a key, with what it means.
type Value struct {
	Name string
	Doc  string
}

// Variant is one accepted shape of an object, chosen by which Discriminator key is present.
type Variant struct {
	Name          string
	Discriminator string
	Fields        []Key
	// ExactlyOne lists keys of which exactly one must be present.
	ExactlyOne []string
}

// Key describes one key of the configuration.
type Key struct {
	Name     string
	Kind     Kind
	Required bool
	Doc      string
	Example  string
	Allowed  []Value // for strings and ints; empty means any
	Pattern  string  // for strings; a regular expression the whole value must match
	Regexp   bool    // for strings: the value is itself a regular expression and must compile
	Default  any     // for ints and strings: the value when the key is omitted
	Min      int     // for ints: the smallest value accepted
	MinItems int     // for lists
	Elem     *Key    // for lists
	Fields   []Key   // for objects with one shape
	Variants []Variant
	Looser   Direction
	// Identity names the fields that identify an entry of a list of objects across versions.
	Identity []string
	// OmittedIsWidest means leaving the key out covers everything, so omitting it never loosens.
	OmittedIsWidest bool
}

// SchemaVersions are the values of `schema:` this hadome reads. A version once accepted is never
// dropped, so a base that was valid stays valid after hadome is upgraded.
var SchemaVersions = []Value{{"1", "The first configuration format."}}

// InvariantTypes are the built-in invariant types.
var InvariantTypes = []Value{
	{"idempotent", "Sending the same request again has the effect of sending it once. Points at a handler or operation. " +
		"A test guards it when it calls the target two or more times."},
	{"isolated", "One user's or tenant's data is never visible to another. Points at a read entry point. " +
		"A test guards it when it calls the target with two different users or tenants: two different arguments " +
		"for the first parameter whose name says user, tenant, owner, account, org, team, workspace, customer or project " +
		"(or else the first that is not a context.Context)."},
	{"round-trip", "Writing out and reading back gives the original. Points at a pair of operations (`targets`). " +
		"A test guards it when it calls both."},
	{"durable", "Whatever was saved successfully can be read after a restart. Points at a save entry point. " +
		"A test guards it when it calls the target and its doc comment says `guards: durable <target>`."},
	{"erasable", "Whatever was deleted remains nowhere. Points at a delete entry point. " +
		"A test guards it when it calls the target and its doc comment says `guards: erasable <target>`."},
	{"append-only", "Written records are never rewritten. Points at a store. " +
		"A test guards it when it calls the target and its doc comment says `guards: append-only <target>`."},
	{"fail-closed", "Broken configuration or credentials stop the system instead of letting things through. " +
		"Points at start-up or authorisation. A test guards it when it calls the target and its doc comment says " +
		"`guards: fail-closed <target>`."},
}

// Languages are the languages hadome has adapters for.
var Languages = []Value{
	{"go", "Go"}, {"typescript", "TypeScript and JavaScript"}, {"python", "Python"},
	{"rust", "Rust"}, {"java", "Java"}, {"kotlin", "Kotlin"},
}

func stringList(name, doc, example string, looser Direction) Key {
	return Key{Name: name, Kind: KindList, Doc: doc, Example: example, Looser: looser, Elem: &Key{Kind: KindString}}
}

// addedEntries is a list that adds entries to a built-in table. Removing one later loosens the check.
func addedEntries(name, what, example string) Key {
	return stringList(name, "Adds "+what+" to the built-in table. Entries can be added, never removed from the built-in table.",
		example, LooserWhenRemoved)
}

const targetPattern = `^[A-Za-z_][A-Za-z0-9_./-]*$`

var invariantKey = Key{
	Name: "invariants", Kind: KindList, Looser: LooserWhenRemoved, Identity: []string{"type", "target", "targets", "id"},
	Doc: "Promises the product keeps. Every enabled invariant must be guarded by at least one active test. " +
		"Removing one needs a person's review.",
	Example: "invariants:\n  - type: idempotent\n    target: api.CreatePost",
	Elem: &Key{Kind: KindObject, Variants: []Variant{
		{
			Name: "built-in", Discriminator: "type", ExactlyOne: []string{"target", "targets"},
			Fields: []Key{
				{Name: "type", Kind: KindString, Required: true, Allowed: InvariantTypes,
					Doc: "A built-in invariant type.", Example: "type: idempotent"},
				{Name: "target", Kind: KindString, Pattern: targetPattern,
					Doc: "What the invariant is about: a function or method, written as <unit>.<name>.", Example: "target: api.CreatePost"},
				{Name: "targets", Kind: KindList, MinItems: 2, Elem: &Key{Kind: KindString, Pattern: targetPattern},
					Doc:     "Two or more functions the invariant is about together, such as an export and an import.",
					Example: "targets: [store.Export, store.Import]"},
			},
		},
		{
			Name: "product-specific", Discriminator: "id",
			Fields: []Key{
				{Name: "id", Kind: KindString, Required: true, Pattern: `^[a-z0-9][a-z0-9-]*$`,
					Doc:     "A name for the invariant. Tests that guard it say `guards: <id>` in their doc comment.",
					Example: "id: billing-1"},
				{Name: "says", Kind: KindString, Required: true,
					Doc: "The promise in one sentence, for people.", Example: `says: "Totals on the screen and in the API match"`},
			},
		},
	}},
}

var gateKey = Key{
	Name: "gates", Kind: KindList, Looser: LooserWhenChanged, Identity: []string{"id"},
	Doc: "Product-specific gates: external commands that look at one version and report findings or items as JSON. " +
		"Removing or changing one needs a person's review.",
	Example: "gates:\n  - id: acme/log-append-only\n    kind: finding\n    run: [scripts/gates/log-append-only]",
	Elem: &Key{Kind: KindObject, Fields: []Key{
		{Name: "id", Kind: KindString, Required: true, Pattern: `^[a-z0-9-]+/[a-z0-9-]+$`,
			Doc: "<namespace>/<name>; the namespace is yours.", Example: "id: acme/log-append-only"},
		{Name: "kind", Kind: KindString, Required: true,
			Allowed: []Value{{"finding", "Reports violations."}, {"measure", "Reports guarded items."}},
			Doc:     "What the gate reports.", Example: "kind: finding"},
		{Name: "run", Kind: KindList, Required: true, MinItems: 1, Elem: &Key{Kind: KindString},
			Doc: "The command and its arguments. A command containing / is a path from the repository root; " +
				"otherwise it is looked up on PATH. It runs in the root of the version being checked.",
			Example: "run: [scripts/gates/log-append-only]"},
		{Name: "paths", Kind: KindList, Elem: &Key{Kind: KindString},
			Doc: "Globs of the files passed to the gate; * stays within one path element, ** spans any number. " +
				"Omitted: every file.", Example: `paths: ["internal/store/log/**"]`},
		{Name: "timeout", Kind: KindString, Pattern: `^[0-9]+(ms|s|m)$`,
			Doc: "How long the gate may run on one version. Default 60s.", Example: "timeout: 30s"},
		{Name: "options", Kind: KindAny,
			Doc: "Anything the gate needs; passed to it untouched.", Example: "options:\n  forbidden: [UPDATE, DELETE]"},
	}},
}

var rulesKey = Key{
	Name: "rules", Kind: KindObject, Doc: "Settings of the built-in rules. Only what differs from the defaults.",
	Example: "rules:\n  tests:\n    io: [example.com/acme/db]",
	Fields: []Key{{
		Name: "tests", Kind: KindObject, Doc: "Settings of the tests/ rules.",
		Example: "tests:\n  hazards:\n    clock: [acme/timeutil.Now]",
		Fields: []Key{
			{Name: "hazards", Kind: KindObject, Doc: "Calls that count as hazards in tests, in addition to the built-in ones.",
				Example: "hazards:\n  sleep: [acme/testutil.Wait]",
				Fields: []Key{
					addedEntries("clock", "functions that read the clock", "clock: [acme/timeutil.Now]"),
					addedEntries("sleep", "functions that wait for time to pass", "sleep: [acme/testutil.Wait]"),
					addedEntries("mock", "import paths of mocking libraries", "mock: [example.com/acme/fakes]"),
				}},
			addedEntries("io", "units that perform input or output, which make every unit importing them not pure; "+
				"end an entry with /... to include everything under it", "io: [example.com/acme/db, example.com/acme/clients/...]"),
			stringList("assertions", "Functions that count as assertions for tests/no-assertion, in addition to the built-in ones. "+
				"Write an entry as the test calls it (acme.MustEqual), or in Go as <import path>.<Name>. "+
				"Entries can be added, never removed from the built-in table.",
				"assertions: [acme.MustEqual]", LooserWhenAdded),
		},
	}, sizeKey, commentsKey, depsKey},
}

var depsKey = Key{
	Name: "deps", Kind: KindObject, Doc: "Settings of the deps/ rules.",
	Example: "deps:\n  diagram: docs/conventions/dependencies.md",
	Fields: []Key{{
		Name: "diagram", Kind: KindString, Pattern: `^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)*\.md$`,
		Doc: "The Markdown file, from the repository root, whose Mermaid flowchart is the dependency diagram. " +
			"Node labels name directories (dir, or dir/... for everything under it); an arrow a --> b lets a import b. " +
			"Without it, the deps/ rules find nothing. Adding an arrow needs a person's review.",
		Example: "diagram: docs/conventions/dependencies.md",
	}},
}

// DefaultTodo is the form a TODO comment must have unless configuration says otherwise.
const DefaultTodo = `^TODO\([^)]+\): \S`

var commentsKey = Key{
	Name: "comments", Kind: KindObject, Doc: "Settings of the comments/ rules.",
	Example: "comments:\n  todo: '^TODO\\(stage[0-9]+\\): \\S'",
	Fields: []Key{{
		Name: "todo", Kind: KindString, Regexp: true, Default: DefaultTodo, Looser: LooserWhenChanged,
		Doc: "A regular expression every comment line starting with TODO, FIXME or XXX must match. Default " +
			"`" + DefaultTodo + "`, that is TODO(<when or who>): <what>. Changing it needs a person's review.",
		Example: "todo: '^TODO\\(stage[0-9]+\\): \\S'",
	}},
}

func limit(name, what string, def int) Key {
	return Key{Name: name, Kind: KindInt, Default: def, Min: 1, Looser: LooserWhenHigher,
		Doc: "The most " + what + " allowed. Default " + strconv.Itoa(def) + ".", Example: name + ": " + strconv.Itoa(def)}
}

var sizeKey = Key{
	Name: "size", Kind: KindObject, Doc: "Limits of the size/ rules. Only product code is measured, never tests or generated code.",
	Example: "size:\n  file-lines: 300",
	Fields: []Key{
		limit("file-lines", "lines in a source file", 400),
		limit("function-lines", "lines in a function or method, from its declaration to its closing brace", 60),
		limit("params", "parameters of a function or method, not counting the receiver", 5),
		limit("complexity", "cyclomatic complexity of a function or method: 1, plus one per if, for, case, && and ||", 15),
		limit("interface-methods", "methods in an interface", 5),
	},
}

// Spec is the specification of `.hadome.yml`.
var Spec = Key{Kind: KindObject, Doc: "The configuration of hadome, `.hadome.yml` at the repository root.", Fields: []Key{
	{Name: "schema", Kind: KindInt, Required: true, Allowed: SchemaVersions,
		Doc: "The configuration format version.", Example: "schema: 1"},
	stringList("extends", "Local files whose settings this file builds on, relative to this file. Later layers add to or "+
		"override earlier ones. Never a URL.", "extends: [../shared/hadome.yml]", LooserNever),
	{Name: "languages", Kind: KindList, Looser: LooserWhenRemoved, OmittedIsWidest: true,
		Elem: &Key{Kind: KindString, Allowed: Languages},
		Doc:  "Languages to check. Detected when omitted.", Example: "languages: [go, typescript]"},
	invariantKey,
	rulesKey,
	gateKey,
}}
