//go:build cgo

package treesitter

// The testing tools a TypeScript test calls that hadome reads: skips from inside a test, fake
// timers, and the matchers after expect.

var (
	// tsFakeTimers turn on a fake clock; tsRealTimers turn it off.
	tsFakeTimers = set("vi.useFakeTimers", "jest.useFakeTimers", "vi.setSystemTime", "jest.setSystemTime", "sinon.useFakeTimers")
	tsRealTimers = set("vi.useRealTimers", "jest.useRealTimers")
	// tsInBodySkips skip the running test from inside it, or every test of a describe block when
	// called in its body: test.skip(), test.skip(cond, reason).
	tsInBodySkips = set("test.skip", "test.fixme")
)

// skips records test.skip() and test.fixme() called in a test or a describe block: with no
// arguments they skip it, with a condition they skip it only in some cases. The reason is the
// description after the condition, or the comment on the line.
func (w *tsWalk) skips(n *node, callee string, args []*node) bool {
	if !tsInBodySkips[callee] {
		return false
	}
	if _, ok := w.tf.declaration(n); ok {
		return false
	}
	reason := ""
	if len(args) > 1 && (args[1].Kind() == "string" || args[1].Kind() == "template_string") {
		reason = unquote(w.tf.f.text(args[1]))
	}
	if reason == "" {
		reason = w.tf.reasonAt(n)
	}
	if len(args) == 0 && !conditional(n, w.t.f.root, tsConditions) {
		w.t.skip(reason, n)
	} else {
		w.t.skipIf(reason, n)
	}
	return true
}

var tsConditions = set("if_statement", "ternary_expression", "switch_case", "binary_expression", "catch_clause")

// timers follows fake timers: after vi.useFakeTimers(), Date and setTimeout read the fake clock.
func (w *tsWalk) timers(callee string) bool {
	switch {
	case tsFakeTimers[callee]:
		w.fake = true
	case tsRealTimers[callee]:
		w.fake = false
	default:
		return false
	}
	return true
}

// matched reports whether expect(x) is followed by a matcher: called (expect(x).toBe(1),
// expect(x).not.toBe(1)), or chai's assertion by property (expect(x).to.be.true). expect(x) alone
// and expect(x).toBeTruthy without the call check nothing.
func (w *tsWalk) matched(call *node) bool {
	n, chai := call, false
	for p := n.Parent(); p != nil && p.Kind() == "member_expression" && isField(p, "object", n); p = n.Parent() {
		n = p
		prop := w.tf.f.field(n, "property")
		chai = chai || prop == "to" || prop == "should"
	}
	if p := n.Parent(); n != call && p != nil && p.Kind() == "call_expression" && isField(p, "function", n) {
		return true
	}
	return chai
}

// tsDiscarded reports whether the value of a call is thrown away: await import('./m'); as a statement.
func tsDiscarded(call *node) bool {
	n := call
	for p := n.Parent(); p != nil; p = n.Parent() {
		switch p.Kind() {
		case "await_expression", "parenthesized_expression":
			n = p
		case "expression_statement":
			return true
		default:
			return false
		}
	}
	return false
}
