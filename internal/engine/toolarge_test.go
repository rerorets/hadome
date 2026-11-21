// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine_test

import (
	"testing"

	"github.com/rerorets/hadome/internal/engine"
)

// bigSource is two in-memory trees and the files each version left out for their size.
type bigSource struct {
	treeSource
	baseBig, headBig []string
}

func (s bigSource) Base() (engine.Version, error) {
	v, err := s.treeSource.Base()
	v.TooLarge = s.baseBig
	return v, err
}

func (s bigSource) Head() (engine.Version, error) {
	v, err := s.treeSource.Head()
	v.TooLarge = s.headBig
	return v, err
}

// viewpoint: state integrity
// target: engine.Engine.Check
// A file head leaves out for its size goes to a person once, with exit code 3, and never blocks:
// one already left out in base, or renamed from one, is not raised again.
func TestAFileTooLargeToReadNeedsReviewOnce(t *testing.T) {
	tree := module("")
	r := check(t, bigSource{treeSource: treeSource{base: tree, head: tree}, headBig: []string{"data/dump.bin"}})
	if r.ExitCode() != engine.ExitReview || len(r.Reviews) != 1 || r.Reviews[0].Rule != engine.TooLargeID ||
		r.Reviews[0].File != "data/dump.bin" {
		t.Fatalf("a new file too large to read must need review; got exit %d, reviews %+v", r.ExitCode(), r.Reviews)
	}
	r = check(t, bigSource{treeSource: treeSource{base: tree, head: tree},
		baseBig: []string{"data/dump.bin"}, headBig: []string{"data/dump.bin"}})
	if r.ExitCode() != engine.ExitOK {
		t.Fatalf("a file already too large in base must pass; got exit %d, reviews %+v", r.ExitCode(), r.Reviews)
	}
	r = check(t, bigSource{treeSource: treeSource{base: tree, head: tree, renames: map[string]string{"data/dump.bin": "data/old.bin"}},
		baseBig: []string{"data/dump.bin"}, headBig: []string{"data/old.bin"}})
	if r.ExitCode() != engine.ExitOK {
		t.Fatalf("a renamed file already too large in base must pass; got exit %d, reviews %+v", r.ExitCode(), r.Reviews)
	}
}
