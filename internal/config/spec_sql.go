package config

// SQLSinkLanguages are the languages whose tests hand their strings to the SQL adapter.
var SQLSinkLanguages = []string{"go", "typescript", "python", "rust", "java", "kotlin"}

// sqlSinksKey adds calls that take SQL strings, per language that hands over test SQL.
var sqlSinksKey = func() Key {
	k := Key{Name: "sql-sinks", Kind: KindObject,
		Doc: "Calls whose arguments take SQL, in addition to the built-in table of each language. A string passed to such a " +
			"call is read as SQL even without a statement head, and a value that is not a string literal passed to it " +
			"counts as unresolved. Write an entry as the test writes the callee: a bare name (run) matches every call " +
			"with that name, a dotted one (db.run) only that callee. Any change needs a person's review.",
		Example: "sql-sinks:\n  typescript: [db.run]\n  python: [run_sql]"}
	for _, lang := range SQLSinkLanguages {
		k.Fields = append(k.Fields, stringList(lang, "Calls that take SQL in "+lang+" tests.",
			lang+": [run_sql]", LooserAlways))
	}
	return k
}()

// SQLDialects are the SQL dialects sql.databases[].dialect accepts: the enabled ones. A dialect is
// enabled once it has passed its trial on real public code (docs/design/adapters-sql.md).
var SQLDialects = []Value{
	{"postgresql", "PostgreSQL."},
}

// sqlKey describes the SQL databases of the repository. Every key that changes how names are resolved
// needs a person's review when it changes, because hadome cannot tell which direction loosens.
var sqlKey = Key{
	Name: "sql", Kind: KindObject,
	Doc: "The SQL databases whose .sql files define objects that tests call. Omitted: every .sql file is one database, " +
		"whose dialect is decided from the markers in its files.",
	Example: "sql:\n  databases:\n    - name: main\n      dialect: postgresql\n      paths: [\"db/**/*.sql\"]",
	Fields: []Key{{
		Name: "databases", Kind: KindList, Looser: LooserAlways, Identity: []string{"name"}, Unique: true,
		Doc: "Databases: sets of objects whose names share one namespace. With two or more, the name is part of the IDs " +
			"of their objects. Adding, removing or changing one needs a person's review, except adding to paths.",
		Example: "databases:\n  - name: main\n    dialect: postgresql\n    paths: [\"db/**/*.sql\"]\n    search-path: [billing, public]",
		Elem: &Key{Kind: KindObject, Fields: []Key{
			{Name: "name", Kind: KindString, Required: true, Pattern: `^[a-z0-9][a-z0-9_-]*$`,
				Doc: "The database's name. With two or more databases, it is part of the unit ID (.sql/<name>).", Example: "name: main"},
			{Name: "dialect", Kind: KindString, Allowed: SQLDialects, Looser: LooserWhenChanged,
				Doc: "The dialect of the database's files. When set, it wins over the markers in the files. Omitted: decided " +
					"from the markers.", Example: "dialect: postgresql"},
			{Name: "paths", Kind: KindList, Elem: &globs, Looser: LooserWhenRemoved,
				Doc: "Globs of the database's .sql files; ** is directories at any depth. Omitted: every .sql file. Removing " +
					"an entry reads fewer files, so it needs a person's review.", Example: `paths: ["db/**/*.sql"]`},
			{Name: "search-path", Kind: KindList, Sequence: true, Elem: &Key{Kind: KindString}, Looser: LooserWhenChanged,
				Doc:     "The schemas unqualified names are searched in, in order, as the database searches them.",
				Example: "search-path: [billing, public]"},
			{Name: "default-schema", Kind: KindString, Looser: LooserWhenChanged,
				Doc:     "The schema of unqualified definitions, for dialects whose connection decides it (MySQL, Oracle, BigQuery).",
				Example: "default-schema: reports"},
			{Name: "catalog", Kind: KindString, Looser: LooserWhenChanged,
				Doc:     "The database (BigQuery: project) name. The database part of qualified names is compared only when it is set.",
				Example: "catalog: example-project"},
			{Name: "order", Kind: KindList, Sequence: true, Elem: &globs, Looser: LooserWhenChanged,
				Doc:     "Globs giving the order the database's files are applied in. Omitted: the natural order of their paths.",
				Example: `order: ["db/migrations/**", "db/views/**"]`},
			{Name: "tests", Kind: KindList, Elem: &globs, Looser: LooserAlways,
				Doc: "Globs of the test files whose SQL is resolved against this database.", Example: `tests: ["warehouse/tests/**"]`},
		}},
	}},
}
