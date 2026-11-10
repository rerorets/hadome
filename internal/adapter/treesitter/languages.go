//go:build cgo

package treesitter

// Languages returns the readers of the five languages.
func Languages() []*lang {
	return []*lang{
		{name: "typescript", breaks: tsBreaks, r: &typescript{}, fresh: func() reader { return &typescript{} }, tables: tsTables, directive: tsDirective.MatchString},
		{name: "python", breaks: pyBreaks, r: &python{}, fresh: func() reader { return &python{} }, tables: pyTables, directive: pyDirective.MatchString},
		{name: "java", breaks: javaBreaks, r: &java{}, fresh: func() reader { return &java{} }, tables: javaTables, directive: javaDirective.MatchString},
		{name: "kotlin", breaks: ktBreaks, r: &kotlin{}, fresh: func() reader { return &kotlin{} }, tables: ktTables, directive: ktDirective.MatchString},
		{name: "rust", breaks: rustBreaks, r: &rust{}, fresh: func() reader { return &rust{} }, tables: rustTables, directive: rustDirective.MatchString},
	}
}
