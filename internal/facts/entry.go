package facts

// Entry is an entry point that can be retried: an HTTP handler, a route. It is either a symbol, or
// code that is not one (a function written where a route is registered), whose calls are listed.
type Entry struct {
	Path   string
	Line   int
	Symbol string
	Calls  []string
	// CallAt maps each ID in Calls to the line of its first call, where an adapter knows it.
	CallAt map[string]int
}
