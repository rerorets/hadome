// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package engine

// TooLargeID is the engine's own report of a file larger than hadome reads. Like LoosenedID it is
// not a rule, so no exception marker can silence it.
const TooLargeID = "unknown/too-large"

// tooLargeReviews returns a review for every file head leaves out for its size that base did not
// already leave out. No rule saw such a file, so it may neither block nor pass unseen.
func tooLargeReviews(base, head Version, renames map[string]string) []Finding {
	before := map[string]bool{}
	for _, p := range base.TooLarge {
		before[p] = true
		if to, ok := renames[p]; ok {
			before[to] = true
		}
	}
	var out []Finding
	for _, p := range head.TooLarge {
		if before[p] {
			continue
		}
		out = append(out, Finding{
			Rule: TooLargeID, File: p, Review: true,
			Message: T("hadome did not read %s: it is larger than 128 MiB", p),
			Why:     T("no rule saw what the file holds, so nothing it adds was checked"),
			Fix:     T("a person checks the file, or keeps a file this large out of the repository"),
		})
	}
	return out
}
