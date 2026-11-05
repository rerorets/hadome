package mutation

import (
	"github.com/rerorets/hadome/internal/engine"
)

// survived is the rule mutation/survived. Its findings are worked out before the check, by trying
// the mutants; the engine then compares them across versions like any rule's: a survivor in head
// with no survivor of the same anchor in base is a regression, and exceptions, review and exit
// codes work as they do for every rule.
// RuleID is the rule hadome mutate reports. hadome check leaves its exception markers alone.
const RuleID = "mutation/survived"

// Rule is the rule without findings, for explaining it (hadome explain mutation/survived).
func Rule() engine.Rule { return survived{} }

type survived struct {
	byVersion map[string][]engine.Finding // version label -> findings
}

func (survived) ID() string { return RuleID }

// Cases is empty: what this rule reports comes from running a project's tests, which only hadome
// mutate does. TestSurvivorsAreComparedLikeFindings pins its comparison down.
func (survived) Cases() []engine.Case { return nil }

func (survived) About() engine.About {
	return engine.About{Finds: engine.T("a break of a function the change touched that no test notices"),
		Why: engine.T("the code can do something else than it does now, and the tests still pass"),
		Fix: engine.T("add or strengthen a test that checks what the broken line decides; if the break cannot change behaviour, " +
			"add an exception with the reason")}
}

func (r survived) Find(in engine.Input) ([]engine.Finding, error) {
	return r.byVersion[in.Version.Label], nil
}
