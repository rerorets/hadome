// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

//go:build cgo

package treesitter

import (
	"strings"

	"github.com/rerorets/hadome/internal/facts"
)

// The grammars' kinds around a value that used looks at: argument lists, what wraps one argument in
// them, and binary expressions.
var (
	argumentLists = set("arguments", "argument_list", "value_arguments")
	argumentWraps = set("value_argument", "parenthesized_expression")
	binaryKinds   = set("binary_expression", "binary_operator", "comparison_operator", "equality_expression",
		"comparison_expression", "additive_expression")
)

// used reports a value that is recognised or calculated with rather than a setting: a URL compared
// with a value or given to startsWith, and, for a duration (isDuration is not nil), one added to a
// time or given to plus. A string joined with + is still a setting, "https://api.shop.internal" +
// path, and so is a duration added to another, 30s + 5s.
func used(f *file, x *node, isDuration func(*node) bool) bool {
	p := x.Parent()
	for p != nil && argumentWraps[p.Kind()] {
		p = p.Parent()
	}
	switch {
	case p == nil:
	case argumentLists[p.Kind()]:
		name := calleeOf(f, p)
		return facts.Compares(name) || isDuration != nil && facts.MovesTime(name)
	case binaryKinds[p.Kind()]:
		op := operator(f, p)
		if facts.Comparison(op) {
			return true
		}
		return isDuration != nil && (op == "+" || op == "-") && !nextToDuration(x, p, isDuration)
	}
	return false
}

// nextToDuration reports another operand of p that is a duration: 30s + 5s builds a setting.
func nextToDuration(x, p *node, isDuration func(*node) bool) bool {
	for _, o := range named(p) {
		if o.Id() != x.Id() && !strings.Contains(o.Kind(), "comment") && isDuration(o) {
			return true
		}
	}
	return false
}

// operator is the first operator token of a binary expression: its first child that is no node of
// its own, so comments and further operands (Python's a < b < c) do not move it.
func operator(f *file, p *node) string {
	for i := uint(0); i < p.ChildCount(); i++ {
		if c := p.Child(i); !c.IsNamed() {
			return strings.TrimSpace(f.text(c))
		}
	}
	return ""
}

// calleeOf returns the last name of the callee an argument list belongs to: startsWith of
// u.startsWith(…), or "".
func calleeOf(f *file, args *node) string {
	call := args.Parent()
	if call == nil || call.StartByte() >= args.StartByte() {
		return ""
	}
	head := strings.TrimSuffix(strings.TrimSpace(string(f.src[call.StartByte():args.StartByte()])), "?.") // f?.(…)
	return lastName(strings.ReplaceAll(head, "?.", "."))
}
