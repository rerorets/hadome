package engine

import (
	"bytes"
	"fmt"
	"io/fs"
	"sort"
	"strings"
)

// mapSource is a Source over two in-memory trees, used by self-tests.
type mapSource struct{ base, head fs.FS }

func (s mapSource) Base() (Version, error)              { return Version{Label: "base", FS: s.base}, nil }
func (s mapSource) Head() (Version, error)              { return Version{Label: "head", FS: s.head}, nil }
func (s mapSource) Renames() (map[string]string, error) { return nil, nil }

// CaseResult is the outcome of one self-test case.
type CaseResult struct {
	Rule string
	Name string
	Err  string
}

// SelfTest runs every case of every rule, each with only its own rule loaded,
// and returns the cases that failed. A rule without cases is itself a failure. A SelfTester
// tests itself instead.
func (e *Engine) SelfTest() []CaseResult {
	var failed []CaseResult
	for _, rule := range e.Rules {
		if st, ok := rule.(SelfTester); ok {
			if err := st.SelfTest(); err != nil {
				failed = append(failed, CaseResult{Rule: rule.ID(), Name: "self-test", Err: err.Error()})
			}
			continue
		}
		cases := rule.Cases()
		if len(cases) == 0 {
			failed = append(failed, CaseResult{Rule: rule.ID(), Err: "rule has no self-test cases"})
		}
		for _, c := range cases {
			one := Engine{Analyzer: e.Analyzer, Rules: []Rule{rule}}
			if msg := runCase(&one, c); msg != "" {
				failed = append(failed, CaseResult{Rule: rule.ID(), Name: c.Name, Err: msg})
			}
		}
	}
	return failed
}

func runCase(e *Engine, c Case) string {
	r, err := e.Check(mapSource{base: c.Base, head: c.Head})
	if err != nil {
		return "check failed: " + err.Error()
	}
	if msg := sameWithoutReuse(e, c, r); msg != "" {
		return msg
	}
	gotFail, gotReview := ids(r.Regressions), ids(r.Reviews)
	for _, m := range r.NewExceptions {
		gotReview = append(gotReview, "exception:"+m.Rule)
	}
	sort.Strings(gotReview)
	want, wantReview := sorted(c.WantFail), sorted(c.WantReview)
	if strings.Join(gotFail, ",") != strings.Join(want, ",") || strings.Join(gotReview, ",") != strings.Join(wantReview, ",") {
		return fmt.Sprintf("want fail %v review %v, got fail %v review %v", want, wantReview, gotFail, gotReview)
	}
	return ""
}

// sameWithoutReuse runs the case again with the analyzer that reuses nothing between versions,
// when the engine's analyzer reuses, and reports how the two reports differ.
func sameWithoutReuse(e *Engine, c Case, r Report) string {
	ru, ok := e.Analyzer.(Reuser)
	if !ok {
		return ""
	}
	plain := *e
	plain.Analyzer = ru.WithoutReuse()
	without, err := plain.Check(mapSource{base: c.Base, head: c.Head})
	if err != nil {
		return "check without reuse failed: " + err.Error()
	}
	var a, b bytes.Buffer
	if err := WriteJSON(&a, r, "en"); err != nil {
		return err.Error()
	}
	if err := WriteJSON(&b, without, "en"); err != nil {
		return err.Error()
	}
	if !bytes.Equal(a.Bytes(), b.Bytes()) {
		return fmt.Sprintf("reuse changed the report:\n with reuse: %s\n without:    %s", a.String(), b.String())
	}
	return ""
}

func ids(fs []Finding) []string {
	out := []string{}
	for _, f := range fs {
		out = append(out, f.Rule)
	}
	sort.Strings(out)
	return out
}

func sorted(s []string) []string {
	out := append([]string{}, s...)
	sort.Strings(out)
	return out
}
