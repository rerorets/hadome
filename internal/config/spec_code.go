// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package config

import "strconv"

// CodeSettings are the settings of the code/ rules.
type CodeSettings struct {
	// ConfigPaths are the globs of rules.code.config-literal.paths: files code/config-literal skips.
	ConfigPaths []string
	// ConfigNames are rules.code.config-literal.names: argument and field names whose literals count.
	ConfigNames []string
	// MinTokens is rules.code.duplicate.min-tokens, the default filled in.
	MinTokens int
}

func codeSettings(code map[string]any) CodeSettings {
	return CodeSettings{
		ConfigPaths: textList(object(code["config-literal"])["paths"]),
		ConfigNames: textList(object(code["config-literal"])["names"]),
		MinTokens:   intOr(object(code["duplicate"])["min-tokens"], DefaultMinTokens),
	}
}

// DefaultMinTokens is the smallest function, in tokens, code/duplicate compares unless configuration
// says otherwise.
const DefaultMinTokens = 50

var codeKey = Key{
	Name: "code", Kind: KindObject, Doc: "Settings of the code/ rules. Only product code is read, never tests or generated code.",
	Example: "code:\n  config-literal:\n    paths: [\"config/**\"]",
	Fields: []Key{{
		Name: "config-literal", Kind: KindObject, Doc: "Settings of code/config-literal.",
		Example: "config-literal:\n  paths: [\"config/**\"]\n  names: [pageSize, max_retries]",
		Fields: []Key{
			stringList("paths", "Globs of the files where the repository keeps its configuration; code/config-literal "+
				"counts no value in them. Adding one needs a person's review.",
				`paths: ["config/**", "internal/settings/**"]`, LooserWhenAdded, globs),
			stringList("names", "Names of arguments, keyword arguments and fields whose literal values belong in "+
				"configuration, such as a page size, a retry count or a bucket. Names are compared in lower case without "+
				"_ and -, so max_retries is maxRetries. Removing one needs a person's review.",
				"names: [pageSize, max_retries, bucket]", LooserWhenRemoved),
		},
	}, {
		Name: "duplicate", Kind: KindObject, Doc: "Settings of code/duplicate and code/diverged-copy.",
		Example: "duplicate:\n  min-tokens: 50",
		Fields: []Key{{Name: "min-tokens", Kind: KindInt, Default: DefaultMinTokens, Min: 1, Looser: LooserWhenHigher,
			Doc: "The fewest tokens a function must have to be compared with others. Default " +
				strconv.Itoa(DefaultMinTokens) + ". Raising it needs a person's review.",
			Example: "min-tokens: 50"}},
	}},
}
