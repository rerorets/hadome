// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package facts

import "testing"

// viewpoint: behaviour
// target: facts.GoBuild
// A Go file's condition is its //go:build expression and the GOOS and GOARCH its name ends with,
// before _test; a name that is only an OS, or ends with a word that is neither, adds nothing.
func TestGoBuildJoinsTheLineAndTheName(t *testing.T) {
	for _, tc := range []struct{ name, expr, want string }{
		{"store.go", "", ""},
		{"store.go", "cgo", "(cgo)"},
		{"run_linux.go", "", "linux"},
		{"run_windows_amd64.go", "", "windows && amd64"},
		{"run_arm64_test.go", "!race", "(!race) && arm64"},
		{"linux.go", "", ""},
		{"run_unix.go", "", ""},
	} {
		if got := GoBuild(tc.name, tc.expr); got != tc.want {
			t.Errorf("GoBuild(%q, %q) = %q, want %q", tc.name, tc.expr, got, tc.want)
		}
	}
}

// viewpoint: behaviour
// target: facts.BuildTogether
// Two conditions can be in one build when some set of tags satisfies both: a tag and its negation
// cannot, two operating systems or two architectures cannot, an OS and an architecture can, and so
// can free tags, wherever they sit in the expressions and however often; no condition, too many
// different tags or one that does not parse counts as able to.
func TestBuildTogetherIsWhetherOneBuildSatisfiesBoth(t *testing.T) {
	many := "a && b && c && d && e && f && g && h && i && j && k && l && m"
	for _, tc := range []struct {
		a, b string
		want bool
	}{
		{"(cgo)", "(!cgo)", false},
		{"(cgo)", "(cgo) && linux", true},
		{"linux", "windows", false},
		{"amd64", "arm64", false},
		{"linux", "amd64", true},
		{"(linux || windows)", "windows && arm64", true},
		{"(unix)", "linux", true},
		{"", "(!cgo)", true},
		{"(cgo)", "", true},
		{many, "!" + "a", true},
		{"(cgo", "(!cgo)", true},
		{"(!cgo)", "linux", true},
		{"(cgo)", "linux", true},
		{"linux", "!windows", true},
		{"(!(!x))", "(!(!x))", true},
		{"(x || y)", "(x || y) && !y", true},
		{"(y || x)", "(y || x) && !y", true},
		{"x && x && x && x && x && x && x", "!x && !x && !x && !x && !x && !x && !x", false},
	} {
		if got := BuildTogether(tc.a, tc.b); got != tc.want {
			t.Errorf("BuildTogether(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}
