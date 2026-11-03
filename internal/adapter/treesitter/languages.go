//go:build cgo

package treesitter

// Languages returns the readers of the five languages.
func Languages() []*lang {
	return []*lang{
		{name: "typescript", r: &typescript{}, tables: tsTables, directive: tsDirective.MatchString},
		{name: "python", r: &python{}, tables: pyTables, directive: pyDirective.MatchString},
		{name: "java", r: &java{}, tables: javaTables, directive: javaDirective.MatchString},
		{name: "kotlin", r: &kotlin{}, tables: ktTables, directive: ktDirective.MatchString},
		{name: "rust", r: &rust{}, tables: rustTables, directive: rustDirective.MatchString},
	}
}
