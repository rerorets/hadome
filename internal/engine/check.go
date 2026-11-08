package engine

import (
	"fmt"
	"slices"
	"sort"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/facts"
)

// Engine runs rules on two versions and keeps only what got worse.
type Engine struct {
	Analyzer Analyzer
	Rules    []Rule
	// Workdir is where shared configuration layers are read from. They live outside the
	// repository, so both versions read the same files.
	Workdir string
	// Candidates, when set, finds invariant candidates in one version. The report lists those
	// new in head.
	Candidates func(in Input) []Candidate
	// ConfigRules, when set, builds more rules from head's configuration (product-specific gates).
	// They run on both versions next to Rules.
	ConfigRules func(c config.Config) ([]Rule, error)
	// Elsewhere, when set, names rules that another command reports: hadome check leaves
	// mutation/survived to hadome mutate, which leaves every other rule to hadome check. Markers
	// for those rules are left alone, neither unknown nor unused.
	Elsewhere func(rule string) bool
}

// side is everything computed for one version.
type side struct {
	input    Input
	findings []Finding
	markers  []Marker
	items    map[string][]Item
}

// Check compares base and head. An error means the check could not run and must not pass.
func (e *Engine) Check(src Source) (Report, error) {
	bv, hv, renames, err := read(src)
	if err != nil {
		return Report{}, err
	}
	baseCfg, err := config.Load(bv.FS, e.Workdir)
	if err != nil {
		return Report{}, fmt.Errorf("configuration of %s:\n%w", bv.Label, err)
	}
	headCfg, err := config.Load(hv.FS, e.Workdir)
	if err != nil {
		return Report{}, fmt.Errorf("configuration of %s:\n%w", hv.Label, err)
	}
	rules := e.Rules
	if e.ConfigRules != nil {
		more, err := e.ConfigRules(headCfg)
		if err != nil {
			return Report{}, fmt.Errorf("configuration of %s:\n%w", hv.Label, err)
		}
		// Built-in rules have their cases checked before hadome is released. Configured rules live
		// only in the user's repository, so a check is the one place to test them before trusting
		// what they report.
		for _, r := range more {
			if st, ok := r.(SelfTester); ok {
				if err := st.SelfTest(); err != nil {
					return Report{}, fmt.Errorf("%s failed its own self-test, so the check does not use it:\n%w", r.ID(), err)
				}
			}
		}
		rules = append(append([]Rule{}, rules...), more...)
	}
	base, err := e.side(bv, config.ForBase(baseCfg, headCfg), rules)
	if err != nil {
		return Report{}, err
	}
	head, err := e.side(hv, headCfg, rules)
	if err != nil {
		return Report{}, err
	}
	r := Report{Messages: headCfg.Messages}
	r.Untouched = compareFindings(&r, base.findings, head.findings, renames)
	r.NewExceptions = newMarkers(base.markers, head.markers, renames)
	if introduces(baseCfg, headCfg) {
		r.Introduced = config.Diff(baseCfg, headCfg)
	} else {
		r.Reviews = append(r.Reviews, loosened(baseCfg, headCfg)...)
	}
	if err := compareItems(&r, rules, base, head); err != nil {
		return Report{}, err
	}
	if e.Candidates != nil {
		r.Candidates = newCandidates(e.Candidates(base.input), e.Candidates(head.input))
	}
	sortFindings(r.Regressions)
	sortFindings(r.Reviews)
	sort.Slice(r.NewExceptions, func(i, j int) bool { return markerLess(r.NewExceptions[i], r.NewExceptions[j]) })
	return r, nil
}

// read reads both versions and the renames between them.
func read(src Source) (Version, Version, map[string]string, error) {
	bv, err := src.Base()
	if err != nil {
		return Version{}, Version{}, nil, fmt.Errorf("reading base: %w", err)
	}
	hv, err := src.Head()
	if err != nil {
		return Version{}, Version{}, nil, fmt.Errorf("reading head: %w", err)
	}
	known, err := src.Renames()
	if err != nil {
		return Version{}, Version{}, nil, fmt.Errorf("reading renames: %w", err)
	}
	renames, err := detectRenames(bv.FS, hv.FS, known)
	if err != nil {
		return Version{}, Version{}, nil, fmt.Errorf("detecting renames: %w", err)
	}
	return bv, hv, renames, nil
}

// compareItems adds the items of measure rules that head lost to the report.
func compareItems(r *Report, rules []Rule, base, head side) error {
	for _, rule := range rules {
		m, ok := rule.(MeasureRule)
		if !ok {
			continue
		}
		lost, err := shrunk(m, base.items[m.ID()], head)
		if err != nil {
			return err
		}
		for _, f := range lost {
			if f.Review {
				r.Reviews = append(r.Reviews, f)
			} else {
				r.Regressions = append(r.Regressions, f)
			}
		}
	}
	return nil
}

func (e *Engine) side(v Version, c config.Config, rules []Rule) (side, error) {
	a, err := e.Analyzer.Analyze(v, c)
	if err != nil {
		return side{}, fmt.Errorf("analysing %s: %w", v.Label, err)
	}
	in := Input{Facts: a, Config: c, Version: v}
	s := side{input: in, items: map[string][]Item{}}
	known := map[string]bool{}
	var raw []Finding
	for _, rule := range rules {
		known[rule.ID()] = true
		var err error
		switch r := rule.(type) {
		case FindingRule:
			var found []Finding
			found, err = r.Find(in)
			raw = append(raw, found...)
		case MeasureRule:
			s.items[r.ID()], err = r.Measure(in)
		default:
			return side{}, fmt.Errorf("rule %s is neither a finding rule nor a measure rule", rule.ID())
		}
		if err != nil {
			return side{}, fmt.Errorf("rule %s on %s: %w", rule.ID(), v.Label, err)
		}
	}
	markers, bad := parseMarkers(e.ownComments(a.Comments), known)
	markTop(markers, a.Comments, v.FS)
	kept, unused := suppress(raw, markers)
	s.findings = append(append(kept, bad...), unused...)
	s.markers = markers
	return s, nil
}

func fingerprint(f Finding, path string) string {
	return f.Rule + "\x00" + path + "\x00" + f.Anchor
}

// compareFindings adds head findings without a match in base to the report and returns how many
// head findings matched base. Colliding fingerprints are matched by count. A match whose Value grew
// is a regression too: the violation got worse.
func compareFindings(r *Report, base, head []Finding, renames map[string]string) int {
	values := map[string][]int{}
	for _, f := range base {
		k := fingerprint(f, renamed(f.File, renames))
		values[k] = append(values[k], f.Value)
	}
	untouched := 0
	sortFindings(head)
	for _, f := range head {
		k := fingerprint(f, f.File)
		if i, ok := bestMatch(values[k], f.Value); ok {
			values[k] = append(values[k][:i], values[k][i+1:]...)
			untouched++
			continue
		}
		if len(values[k]) > 0 { // the violation exists in base but grew: it used up the closest one
			was := slices.Max(values[k])
			i := slices.Index(values[k], was)
			values[k] = append(values[k][:i], values[k][i+1:]...)
			f.Message = T("%s; grew from %d", f.Message, was)
		}
		if f.Review {
			r.Reviews = append(r.Reviews, f)
		} else {
			r.Regressions = append(r.Regressions, f)
		}
	}
	return untouched
}

// bestMatch returns the smallest base value that is at least v, so that each head finding takes
// the base finding it fits most tightly.
func bestMatch(base []int, v int) (int, bool) {
	best := -1
	for i, b := range base {
		if b >= v && (best < 0 || b < base[best]) {
			best = i
		}
	}
	return best, best >= 0
}

// newMarkers returns head markers with no match in base. A changed reason counts as new.
func newMarkers(base, head []Marker, renames map[string]string) []Marker {
	count := map[string]int{}
	for _, m := range base {
		count[m.Rule+"\x00"+renamed(m.File, renames)+"\x00"+m.Reason]++
	}
	var out []Marker
	for _, m := range head {
		k := m.Rule + "\x00" + m.File + "\x00" + m.Reason
		if count[k] > 0 {
			count[k]--
			continue
		}
		out = append(out, m)
	}
	return out
}

// shrunk returns a finding for every base item missing from head and not removed legitimately.
func shrunk(rule MeasureRule, base []Item, head side) ([]Finding, error) {
	present := map[string]bool{}
	for _, it := range head.items[rule.ID()] {
		present[it.Key] = true
	}
	var out []Finding
	for _, it := range base {
		if present[it.Key] {
			continue
		}
		legit, err := rule.RemovedLegitimately(it, head.input)
		if err != nil {
			return nil, fmt.Errorf("rule %s: %w", rule.ID(), err)
		}
		if legit {
			continue
		}
		present[it.Key] = true // report each key once
		if e, ok := rule.(LossExplainer); ok {
			it = e.Lost(it, head.input)
		}
		out = append(out, Finding{
			Rule: rule.ID(), File: it.File, Line: it.Line, Anchor: it.Key,
			Message: it.Detail, Why: it.Why, Fix: it.Fix, Review: it.Review,
		})
	}
	return out, nil
}

// newCandidates returns the head candidates that were not candidates in base.
func newCandidates(base, head []Candidate) []Candidate {
	seen := map[string]bool{}
	for _, c := range base {
		seen[c.Key()] = true
	}
	var out []Candidate
	for _, c := range head {
		if !seen[c.Key()] {
			out = append(out, c)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key() < out[j].Key() })
	return out
}

func renamed(path string, renames map[string]string) string {
	if to, ok := renames[path]; ok {
		return to
	}
	return path
}

func sortFindings(fs []Finding) {
	sort.Slice(fs, func(i, j int) bool {
		a, b := fs[i], fs[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.File != b.File {
			return a.File < b.File
		}
		if a.Line != b.Line {
			return a.Line < b.Line
		}
		return a.Anchor < b.Anchor
	})
}

func markerLess(a, b Marker) bool {
	if a.File != b.File {
		return a.File < b.File
	}
	if a.Line != b.Line {
		return a.Line < b.Line
	}
	return a.Rule < b.Rule
}

// ownComments leaves out the comments whose marker names a rule reported elsewhere.
func (e *Engine) ownComments(cs []facts.Comment) []facts.Comment {
	if e.Elsewhere == nil {
		return cs
	}
	var out []facts.Comment
	for _, c := range cs {
		if m := markerPattern.FindStringSubmatch(c.Text); m != nil && e.Elsewhere(m[1]) {
			continue
		}
		out = append(out, c)
	}
	return out
}
