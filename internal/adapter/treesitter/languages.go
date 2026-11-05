//go:build cgo

package treesitter

// Languages returns the readers of the five languages.
func Languages() []*lang {
	return []*lang{
		{name: "typescript", breaks: tsBreaks, r: &typescript{}, tables: tsTables, directive: tsDirective.MatchString},
		{name: "python", breaks: pyBreaks, r: &python{}, tables: pyTables, directive: pyDirective.MatchString},
		{name: "java", breaks: javaBreaks, r: &java{}, tables: javaTables, directive: javaDirective.MatchString},
		{name: "kotlin", breaks: ktBreaks, r: &kotlin{}, tables: ktTables, directive: ktDirective.MatchString},
		{name: "rust", breaks: rustBreaks, r: &rust{}, tables: rustTables, directive: rustDirective.MatchString},
	}
}
