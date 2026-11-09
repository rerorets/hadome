package sql

import (
	"sort"
	"strings"
)

// Dialect reads one SQL dialect: which files are written in it, how its files split into what the
// grammar reads, and which objects a tree defines and names. Adding a dialect is adding one
// implementation and registering it in dialects.
type Dialect interface {
	Name() string
	// Markers returns the dialect's markers found in src outside strings and comments.
	Markers(src []byte) []Marker
	// Split removes what the client reads, not the database (psql meta-commands, DELIMITER, GO, /),
	// and returns the pieces the grammar reads. Removed text is blanked, so offsets stay the file's.
	Split(src []byte) []Statement
	Parse(s Statement) (*Node, error)
	// Definitions returns what each statement of the tree does to an object, in order.
	Definitions(t *Node) []Definition
	// References returns the names the statements of the tree use, in name positions only.
	References(t *Node) []Reference
	// Fold normalizes one part of a name by the dialect's rules for case and quotes.
	Fold(id Ident) string
	Builtins() Builtins
	Hazards() []HazardEntry
}

// dialectEntry is one row of the dialect table. Enabled is true once the dialect has passed its trial
// on real public code; a dialect that is not enabled cannot be chosen, and files whose markers
// point to it are unread.
type dialectEntry struct {
	Dialect Dialect
	Enabled bool
}

// dialects are every dialect hadome has a reader for, in the order markers are reported.
var dialects = []dialectEntry{
	{postgresqlDialect, true},
	{mysqlDialect, false},
	{sqliteDialect, false},
	{tsqlDialect, false},
	{oracleDialect, false},
	{bigqueryDialect, false},
	{snowflakeDialect, false},
}

// dialectNamed returns the dialect with the name and whether it is enabled.
func dialectNamed(name string) (Dialect, bool) {
	for _, e := range dialects {
		if e.Dialect.Name() == name {
			return e.Dialect, e.Enabled
		}
	}
	return nil, false
}

// EnabledDialects returns the names of the dialects that can be chosen, sorted.
func EnabledDialects() []string {
	var out []string
	for _, e := range dialects {
		if e.Enabled {
			out = append(out, e.Dialect.Name())
		}
	}
	sort.Strings(out)
	return out
}

// Marker is one piece of text that only a dialect writes.
type Marker struct {
	Line int
	Text string
}

// Statement is a piece of a file the grammar reads at once: a whole file, or one batch between
// client separators (GO, /). Text has the file's offsets: Start is where it begins.
type Statement struct {
	Text  string
	Start int
	Line  int
}

// Ident is one part of a name as written.
type Ident struct {
	Text   string // without quotes
	Quoted bool
}

// QualifiedName is a name with its parts, outermost first: schema.name, catalog.schema.name.
type QualifiedName []Ident

// Effect is what a statement does to an object.
type Effect int

// Effects of statements.
const (
	EffectCreate Effect = iota
	EffectAlter
	EffectRename
	EffectDrop
	// EffectDoc gives an object its doc without changing it (COMMENT ON).
	EffectDoc
)

// Definition is what one statement does to one object.
type Definition struct {
	Kind   ObjectKind
	Effect Effect
	Name   QualifiedName
	// NewName is the name after a rename. A part left out keeps the old name's part.
	NewName QualifiedName
	Params  []string
	// Events are the writes a trigger runs on (INSERT, UPDATE, DELETE), upper-cased.
	Events []string
	// On is the table a trigger is on.
	On QualifiedName
	// Executes is the routine a PostgreSQL trigger executes.
	Executes QualifiedName
	// Temp is true for an object that exists only while the script runs.
	Temp bool
	// Package is true for a routine declared in an Oracle package specification: Name is then
	// schema.package.name.
	Package bool
	Line    int
	EndLine int
	// Start is the statement's byte offset, so that a doc comment right before it can be found.
	Start int
}

// Use is how a test uses a name.
type Use int

// Uses of names.
const (
	UseCall  Use = iota // a function call, CALL, EXEC
	UseRead             // a relation read
	UseWrite            // a relation written: INSERT, UPDATE, DELETE, MERGE, TRUNCATE
)

// Reference is one name in a name position.
type Reference struct {
	Name QualifiedName
	Use  Use
	// Event is the write (INSERT, UPDATE, DELETE) when Use is UseWrite.
	Event string
	// Args are the texts of the arguments of a call, in order.
	Args []string
	Line int
	// Hole is true when the name is a hole of the host language (${…}), which no name can match.
	Hole bool
}

// Builtins are what the dialect provides without a definition.
type Builtins struct {
	// Functions are built-in functions, folded.
	Functions map[string]bool
	// Schemas are system schemas, folded: every name in them is built in.
	Schemas map[string]bool
	// Prefixes start the names of built-in relations and functions, folded (pg_ in PostgreSQL).
	Prefixes []string
}

// Has reports whether an unqualified name, folded, is built in.
func (b Builtins) Has(name string) bool {
	if b.Functions[name] {
		return true
	}
	for _, p := range b.Prefixes {
		if strings.HasPrefix(name, p) {
			return true
		}
	}
	return false
}

// HazardEntry is one name or keyword sequence of the dialect that makes a test untrustworthy.
type HazardEntry struct {
	Category string
	// Call is a function name, folded and qualified as written (now, dbms_lock.sleep); empty for a
	// keyword sequence.
	Call string
	// Keywords is a keyword sequence (CURRENT_TIMESTAMP, WAITFOR DELAY), upper-cased.
	Keywords []string
}
