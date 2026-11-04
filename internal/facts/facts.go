// Package facts holds the language-neutral facts that language adapters extract from one version
// of a repository, and that rules consume. Nothing here knows any language.
package facts

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// Role is what a file is for.
type Role string

// Roles a file can have.
const (
	RoleSource    Role = "source"
	RoleTest      Role = "test"
	RoleGenerated Role = "generated"
	RoleOther     Role = "other"
)

// HazardCategory is a language-neutral name for a test-writing habit that makes tests untrustworthy.
type HazardCategory string

// Hazard categories that adapters map their language's APIs to.
const (
	HazardClock      HazardCategory = "clock"
	HazardSleep      HazardCategory = "sleep"
	HazardMock       HazardCategory = "mock"
	HazardAlwaysTrue HazardCategory = "always-true"
)

// File is one file of a version.
type File struct {
	Path     string
	Language string
	Role     Role
	Lines    int
	// Unit is what a dependency diagram draws a box for, as imports name it: in Go, the package's
	// import path. Empty for files that belong to no unit.
	Unit string
	// UnitPath is where the unit is in the repository, the name a dependency diagram uses: a
	// directory, or for Rust a module's path such as src/store.
	UnitPath string
	// Unread is true for a file of a supported language that could not be read: it does not parse,
	// or this build of hadome has no reader for its language.
	Unread bool
}

// Import is one import of a non-test file: what the unit itself depends on.
type Import struct {
	Path     string // the importing file
	Line     int
	FromUnit string
	ToUnit   string
	// Internal is true when ToUnit is one of the repository's own units.
	Internal bool
	// IO is true when ToUnit performs input or output, by the adapter's io table and configuration.
	IO bool
}

// Comment is one comment, line or block. Text excludes the comment delimiters.
type Comment struct {
	Path string
	Line int
	Text string
	// Directive is true for instructions to a tool written as comments, such as //go:embed.
	// They are not prose, so rules about what comments say skip them.
	Directive bool
}

// SymbolKind is what a symbol is.
type SymbolKind string

// Symbol kinds.
const (
	SymbolFunction  SymbolKind = "function"
	SymbolMethod    SymbolKind = "method"
	SymbolInterface SymbolKind = "interface"
	SymbolType      SymbolKind = "type" // a named type that is not an interface
)

// Symbol is a function, method or named type of non-test, non-generated code. ID is unique within a
// version and stable across versions as long as the symbol keeps its name and unit.
type Symbol struct {
	ID      string
	Kind    SymbolKind
	Path    string
	Line    int
	EndLine int
	// Exported is true for symbols other units can use.
	Exported bool
	// Doc is true when the symbol has a doc comment.
	Doc bool
	// Scoped is true when a parameter says whose data the symbol works on (a user, a tenant, …).
	Scoped bool
	// Params counts the parameters of a function or method, without the receiver.
	Params int
	// Complexity is the cyclomatic complexity of a function or method.
	Complexity int
	// Methods counts the methods of an interface.
	Methods int
}

// Hazard is one occurrence of a hazard category. Evidence is the normalised source text
// that triggered it; it is used to recognise the same hazard when lines move.
type Hazard struct {
	Category HazardCategory
	// Path is the file the hazard is in. It differs from the test's file when the hazard is in a
	// helper function the test calls.
	Path     string
	Line     int
	Evidence string
}

// Skip describes whether a test is skipped, always or only in some cases. Reason is empty when no
// reason was given.
type Skip struct {
	// Skipped is true when the test never runs. A skipped test guards nothing.
	Skipped bool
	// Conditional is true when the test skips itself only in some cases (on one platform, say). It
	// still runs and guards, but the skip still needs a reason.
	Conditional bool
	Reason      string
	Line        int
}

// Test is one test function.
type Test struct {
	Name string
	Path string
	Line int
	// Hazards lists each hazard once, even when a helper holding it is called several times.
	Hazards []Hazard
	Skip    Skip
	// Calls are the symbols the test calls, as far as the adapter could identify them, one per
	// target, sorted by target.
	Calls []Call
	// Unresolved counts calls whose target could not be identified.
	Unresolved int
	// Reads counts uses of product values that are not calls: a constant, a variable, or a function
	// passed as a value. Names in type positions are not values.
	Reads int
	// Asserts counts the calls that check something: assertions, and calls that fail the test.
	Asserts int
	// AssertsUnknown is true when the test hands its testing tool (Go's t, Python's self) to code the
	// adapter cannot read, which may check something there.
	AssertsUnknown bool
	// Body is a fingerprint of the test's tokens without its name, comments and whitespace (see
	// Fingerprint). Literals are kept, so tests with different inputs differ. Empty when unknown.
	Body string
	// Declared holds the "key: value" lines of the test's doc comment, values by key, in order.
	Declared map[string][]string
}

// Call is one symbol a test calls.
type Call struct {
	// Target is the symbol's ID.
	Target string
	// Times counts call sites; one inside a loop counts twice, because it runs more than once.
	Times int
	// Scopes counts the distinct arguments passed where the user or tenant goes. Zero when the
	// target takes no arguments.
	Scopes int
}

// scopeWords mark a parameter that says whose data a call works on.
var scopeWords = []string{"user", "tenant", "owner", "account", "org", "team", "workspace", "customer", "project"}

// IsScopeName reports whether a parameter name says whose data a call works on: a user, a tenant,
// an owner and the like. docs/design/rules-tests.md calls such a parameter the scope.
func IsScopeName(name string) bool {
	name = strings.ToLower(name)
	for _, w := range scopeWords {
		if strings.Contains(name, w) {
			return true
		}
	}
	return false
}

// Analysis is everything adapters extracted from one version. Slices are sorted by path, then line.
type Analysis struct {
	Files    []File
	Comments []Comment
	Symbols  []Symbol
	Imports  []Import
	Tests    []Test
}

// HasSymbol reports whether a symbol with this ID exists in the version.
func (a *Analysis) HasSymbol(id string) bool {
	for _, s := range a.Symbols {
		if s.ID == id {
			return true
		}
	}
	return false
}

// Fingerprint is the Body of a test made of these tokens. Every adapter uses it, so that the same
// tokens give the same fingerprint in every language.
func Fingerprint(tokens []string) string {
	h := sha256.New()
	for _, t := range tokens {
		h.Write([]byte(t))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil)[:16])
}
