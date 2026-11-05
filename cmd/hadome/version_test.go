package main

import (
	"runtime/debug"
	"testing"
)

// Installed with go get -tool, hadome is built from source without the release's stamped version,
// so the version Go embedded is what tells a user which hadome they pinned.
func TestVersionComesFromTheModuleWhenNothingIsStamped(t *testing.T) {
	built := func(path, v string) *debug.BuildInfo {
		return &debug.BuildInfo{Main: debug.Module{Path: path, Version: v}}
	}
	cases := []struct {
		name    string
		stamped string
		info    *debug.BuildInfo
		ok      bool
		want    string
	}{
		{"a release build keeps the stamped version", "1.2.0", built("github.com/rerorets/hadome", "v9.9.9"), true, "1.2.0"},
		{"go get -tool gives the module version", "dev", built("github.com/rerorets/hadome", "v0.2.0"), true, "0.2.0"},
		{"a pseudo-version is printed as it is", "dev", built("github.com/rerorets/hadome", "v0.2.1-0.20261105000000-abcdef123456"), true, "0.2.1-0.20261105000000-abcdef123456"},
		{"a build in this repository is dev", "dev", built("github.com/rerorets/hadome", "(devel)"), true, "dev"},
		{"another module's version is not hadome's", "dev", built("example.com/other", "v3.0.0"), true, "dev"},
		{"no build information is dev", "dev", nil, false, "dev"},
	}
	for _, c := range cases {
		if got := versionOf(c.stamped, c.info, c.ok); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}
