package engine_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
)

// lostItems is a measure rule whose base items are all missing from head. It hands its items over
// in the order given, which is deliberately not the report's order.
type lostItems struct {
	id    string
	items []engine.Item
}

func (r lostItems) ID() string           { return r.id }
func (r lostItems) Cases() []engine.Case { return nil }
func (r lostItems) Measure(in engine.Input) ([]engine.Item, error) {
	if in.Version.Label != "base" {
		return nil, nil
	}
	return r.items, nil
}
func (r lostItems) RemovedLegitimately(engine.Item, engine.Input) (bool, error) { return false, nil }

func lost(file string, line int, review bool) engine.Item {
	return engine.Item{Key: fmt.Sprintf("%s:%d", file, line), File: file, Line: line,
		Detail: engine.Verbatim("lost"), Review: review}
}

func places(fs []engine.Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, fmt.Sprintf("%s %s:%d", f.Rule, f.File, f.Line))
	}
	return out
}

// viewpoint: invariant
// target: engine.Engine.Check
// Engine invariant (determinism): regressions and reviews come out ordered by rule, then file, then
// line, whatever order the rules run in and hand over what they found.
func TestFindingsComeOutInRuleFileLineOrderWhateverOrderRulesReportThem(t *testing.T) {
	later := lostItems{id: "zz/lost", items: []engine.Item{lost("b.go", 1, false), lost("a.go", 9, true), lost("a.go", 2, false)}}
	earlier := lostItems{id: "aa/lost", items: []engine.Item{lost("b.go", 3, true), lost("b.go", 1, true), lost("a.go", 5, false)}}
	e := &engine.Engine{Analyzer: adapter.New(), Rules: []engine.Rule{later, earlier}}
	tree := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
	r, err := e.Check(treeSource{base: tree, head: tree})
	if err != nil {
		t.Fatal(err)
	}
	wantRegressions := []string{"aa/lost a.go:5", "zz/lost a.go:2", "zz/lost b.go:1"}
	if got := places(r.Regressions); !reflect.DeepEqual(got, wantRegressions) {
		t.Errorf("regressions: want %v, got %v", wantRegressions, got)
	}
	wantReviews := []string{"aa/lost b.go:1", "aa/lost b.go:3", "zz/lost a.go:9"}
	if got := places(r.Reviews); !reflect.DeepEqual(got, wantReviews) {
		t.Errorf("reviews: want %v, got %v", wantReviews, got)
	}
}

// viewpoint: invariant
// target: engine.Engine.Check
// Engine invariant (determinism): new exceptions come out ordered by file, then line, then rule, so
// two markers on one line come out the same way whichever is written first.
func TestNewExceptionsOnOneLineComeOutInRuleOrder(t *testing.T) {
	head := module("\t/* gate:allow tests/no-sleep reason: one */ /* gate:allow tests/no-clock reason: two */\n")
	r := check(t, treeSource{base: module(""), head: head})
	var got []string
	for _, m := range r.NewExceptions {
		got = append(got, m.Rule)
	}
	want := []string{"tests/no-clock", "tests/no-sleep"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("want new exceptions %v, got %v (%+v)", want, got, r.NewExceptions)
	}
}

// viewpoint: external contract
// target: engine.WriteJSON
// Every list in the JSON report is an empty list when there is nothing in it, never null, so an
// agent can iterate over any key without checking for null first.
func TestJSONListsAreEmptyNotNull(t *testing.T) {
	for name, r := range map[string]engine.Report{
		"empty report":    {},
		"a passing check": check(t, treeSource{base: module(""), head: module("")}),
	} {
		var out bytes.Buffer
		if err := engine.WriteJSON(&out, r, "en"); err != nil {
			t.Fatal(err)
		}
		var m map[string]json.RawMessage
		if err := json.Unmarshal(out.Bytes(), &m); err != nil {
			t.Fatalf("%s: not JSON: %v", name, err)
		}
		for _, k := range []string{"regressions", "reviews", "new_exceptions", "candidates", "introduced"} {
			if string(m[k]) != "[]" {
				t.Errorf("%s: %s must be [], got %s", name, k, m[k])
			}
		}
	}
}

// viewpoint: external contract
// target: engine.WriteJSON
// New exceptions and candidates reach the JSON as they are in the report.
func TestJSONCarriesNewExceptionsAndCandidates(t *testing.T) {
	r := engine.Report{
		NewExceptions: []engine.Marker{{Rule: "tests/no-clock", Reason: "measures wall time", File: "a.go", Line: 3}},
		Candidates:    []engine.Candidate{{Type: "erasable", Targets: []string{"store.DeleteRow"}, File: "store/delete.go", Line: 4}},
	}
	var out bytes.Buffer
	if err := engine.WriteJSON(&out, r, "en"); err != nil {
		t.Fatal(err)
	}
	var m struct {
		NewExceptions []map[string]any `json:"new_exceptions"`
		Candidates    []map[string]any `json:"candidates"`
	}
	if err := json.Unmarshal(out.Bytes(), &m); err != nil {
		t.Fatal(err)
	}
	if len(m.NewExceptions) != 1 || m.NewExceptions[0]["reason"] != "measures wall time" {
		t.Errorf("want the one new exception, got %v", m.NewExceptions)
	}
	if len(m.Candidates) != 1 {
		t.Errorf("want the one candidate, got %v", m.Candidates)
	}
}
