package golang

import (
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// tables are the hazard tables: the built-in entries plus those the configuration adds.
// Configuration can add entries, never remove them.
type tables struct {
	calls map[string]facts.HazardCategory // "<import path>.<Name>" -> category
	mocks map[string]bool                 // import paths of mocking libraries
	io    ioTable
	// asserts are the functions configuration adds to the assertions, "<import path>.<Name>".
	asserts map[string]bool
	// sinks are the calls configuration adds to those that take SQL: a function or method name, or a
	// callee as written (db.Run).
	sinks map[string]bool
}

func newTables(c config.Config) *tables {
	t := &tables{
		calls: map[string]facts.HazardCategory{
			"time.Now": facts.HazardClock, "time.Since": facts.HazardClock, "time.Until": facts.HazardClock,
			"time.Sleep": facts.HazardSleep,
		},
		mocks: map[string]bool{
			"github.com/golang/mock/gomock":      true,
			"go.uber.org/mock/gomock":            true,
			"github.com/stretchr/testify/mock":   true,
			"github.com/vektra/mockery/v2/mocks": true,
		},
		io:      newIOTable(c.IO),
		asserts: map[string]bool{},
		sinks:   map[string]bool{},
	}
	for _, e := range c.SQLSinks["go"] {
		t.sinks[e] = true
	}
	for _, e := range c.Assertions {
		t.asserts[e] = true
	}
	for _, e := range c.Hazards["clock"] {
		t.calls[e] = facts.HazardClock
	}
	for _, e := range c.Hazards["sleep"] {
		t.calls[e] = facts.HazardSleep
	}
	for _, e := range c.Hazards["mock"] {
		t.mocks[e] = true
	}
	return t
}
