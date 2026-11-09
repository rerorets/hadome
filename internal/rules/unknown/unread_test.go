package unknown_test

import (
	"strings"
	"testing"

	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/facts"
	"github.com/rerorets/hadome/internal/rules/unknown"
)

// viewpoint: external contract
// target: unknown/unread
// An unread SQL file is explained by its dialect or a statement that does not parse, and fixed in
// sql.databases; an unread file of another language by a parse error or a build without its reader.
func TestUnreadFilesSayWhyByLanguage(t *testing.T) {
	var rule engine.FindingRule
	for _, r := range unknown.All() {
		if r.ID() == "unknown/unread" {
			rule = r.(engine.FindingRule)
		}
	}
	got, err := rule.Find(engine.Input{Facts: &facts.Analysis{Files: []facts.File{
		{Path: "db/a.sql", Language: "sql", Unread: true}, {Path: "store/a.go", Language: "go", Unread: true}}}})
	if err != nil || len(got) != 2 {
		t.Fatalf("findings %+v, error %v", got, err)
	}
	if why, fix := got[0].Why.In("en"), got[0].Fix.In("en"); !strings.Contains(why, "dialect") || !strings.Contains(fix, "sql.databases") {
		t.Errorf("SQL: why %q, fix %q", why, fix)
	}
	if why := got[1].Why.In("en"); strings.Contains(why, "dialect") || !strings.Contains(why, "reader for go") {
		t.Errorf("Go: why %q", why)
	}
}
