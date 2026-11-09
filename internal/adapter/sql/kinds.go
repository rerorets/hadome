package sql

import "github.com/rerorets/hadome/internal/facts"

// ObjectKind is a kind of database object. Adding a kind is adding one row to objectKinds.
type ObjectKind string

// Object kinds the dialects define.
const (
	KindFunction    ObjectKind = "function"
	KindProcedure   ObjectKind = "procedure"
	KindView        ObjectKind = "view"
	KindMatView     ObjectKind = "materialized view"
	KindTrigger     ObjectKind = "trigger"
	KindTask        ObjectKind = "task"
	KindTable       ObjectKind = "table"
	KindSequence    ObjectKind = "sequence"
	KindStream      ObjectKind = "stream"
	KindPackage     ObjectKind = "package"
	KindPackageBody ObjectKind = "package body"
	// KindOther is an object no test runs by naming it: an index, a type, a stage. It becomes no
	// symbol, but a rename or a drop of it is still read so that nothing is mistaken for it.
	KindOther ObjectKind = "other"
)

// kindRow says what an object kind becomes: its symbol kind ("" for none) and whether a test naming
// it calls it or reads it.
type kindRow struct {
	Symbol facts.SymbolKind
	Calls  bool
}

// objectKinds decide, for each kind, whether it becomes a symbol and how tests count it. The test is
// whether naming it in a test runs product logic.
var objectKinds = map[ObjectKind]kindRow{
	KindFunction:  {facts.SymbolRoutine, true},
	KindProcedure: {facts.SymbolRoutine, true},
	KindView:      {facts.SymbolRelation, true},
	KindMatView:   {facts.SymbolRelation, true},
	KindTrigger:   {facts.SymbolRoutine, true},
	KindTask:      {facts.SymbolRoutine, true},
	KindTable:     {facts.SymbolRelation, false},
	KindSequence:  {facts.SymbolRelation, false},
	KindStream:    {facts.SymbolRelation, false},
	// A package is a namespace: the routines of its specification are the symbols.
	KindPackage:     {"", false},
	KindPackageBody: {"", false},
	KindOther:       {"", false},
}
