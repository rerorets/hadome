package config

// Analysis is the part of the configuration adapters read: only settings that can change the
// facts. Adapters receive it instead of Config, so that no other setting can change what they
// report, and the session that reuses their results keys them by exactly this.
//
// Design: docs/design/analysis-reuse.md
type Analysis struct {
	Languages      []string
	Hazards        map[string][]string
	IO             []string
	Assertions     []string
	TestFiles      []string
	GeneratedFiles []string
	ProductFiles   []string
	AnsibleFiles   []string
	Databases      []Database
	SQLSinks       map[string][]string
}

// Analysis returns the part of the configuration adapters read.
func (c Config) Analysis() Analysis {
	return Analysis{
		Languages: c.Languages, Hazards: c.Hazards, IO: c.IO, Assertions: c.Assertions,
		TestFiles: c.TestFiles, GeneratedFiles: c.GeneratedFiles, ProductFiles: c.ProductFiles,
		AnsibleFiles: c.AnsibleFiles, Databases: c.Databases, SQLSinks: c.SQLSinks,
	}
}
