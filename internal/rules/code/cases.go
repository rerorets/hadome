// SPDX-License-Identifier: MIT
// Copyright (c) 2026 rerorets

package code

import (
	"strings"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

// module is a Go module with the given files, keyed by path; a later set replaces an earlier one's
// file of the same path.
func module(sets ...map[string]string) fstest.MapFS {
	t := fstest.MapFS{"go.mod": {Data: []byte("module example.com/app\n")}}
	for _, files := range sets {
		for p, s := range files {
			t[p] = &fstest.MapFile{Data: []byte(s)}
		}
	}
	return t
}

// client is a file of package client with the given imports and declarations.
func client(imports string, decls ...string) map[string]string {
	head := "package client\n\n"
	if imports != "" {
		head += "import (\n" + imports + ")\n\n"
	}
	return map[string]string{"client/client.go": head + strings.Join(decls, "\n")}
}

const fetchOne = `func Fetch() string {
	return get("https://api.shop.internal/v1/orders")
}

func get(u string) string { return u }
`

const fetchTwo = `func Fetch() string {
	a := get("https://api.shop.internal/v1/orders")
	return a + get("https://api.shop.internal/v1/items")
}

func get(u string) string { return u }
`

func configLiteralCases() []engine.Case {
	none := client("", "func Fetch() string {\n\treturn get(\"/v1/orders\")\n}\n\nfunc get(u string) string { return u }\n")
	one := client("", fetchOne)
	edited := client("", "func Fetch() string {\n\tu := get(\"https://api.shop.internal/v1/orders\")\n\treturn u\n}\n\nfunc get(u string) string { return u }\n")
	moved := client("", "func get(u string) string { return u }\n\n"+fetchOne)
	constant := client("", "const orders = \"https://api.shop.internal/v1/orders\"\n\nfunc Fetch() string {\n\treturn get(orders)\n}\n\nfunc get(u string) string { return u }\n")
	local := client("", "func Fetch() string {\n\tconst orders = \"https://api.shop.internal/v1/orders\"\n\treturn get(orders)\n}\n\nfunc get(u string) string { return u }\n")
	namespace := client("", "func Fetch() string {\n\treturn get(\"http://www.w3.org/2000/svg\")\n}\n\nfunc get(u string) string { return u }\n")
	sentence := client("", "func Fetch() string {\n\treturn get(\"see https://example.com/docs for more\")\n}\n\nfunc get(u string) string { return u }\n")
	timeout := client("\t\"net/http\"\n\t\"time\"\n", "func New() *http.Client {\n\treturn &http.Client{Timeout: 30 * time.Second}\n}\n")
	noTimeout := client("\t\"net/http\"\n", "func New() *http.Client {\n\treturn &http.Client{}\n}\n")
	addr := client("", "func Addr() string {\n\treturn \"db.internal:5432\"\n}\n")
	noAddr := client("", "func Addr() string {\n\treturn \"\"\n}\n")
	page := client("", "type Query struct{ PageSize int }\n\nfunc New() Query {\n\treturn Query{PageSize: 50}\n}\n")
	noPage := client("", "type Query struct{ PageSize int }\n\nfunc New() Query {\n\treturn Query{}\n}\n")
	names := map[string]string{".hadome.yml": "schema: 1\nrules:\n  code:\n    config-literal:\n      names: [page_size]\n"}
	paths := map[string]string{".hadome.yml": "schema: 1\nrules:\n  code:\n    config-literal:\n      paths: [\"client/**\"]\n"}
	test := map[string]string{"client/client.go": "package client\n",
		"client/client_test.go": "package client\n\nimport \"testing\"\n\nfunc TestFetch(t *testing.T) {\n\tif \"https://api.shop.internal\" == \"\" {\n\t\tt.Fatal()\n\t}\n}\n"}
	marked := client("", "func Fetch() string {\n\t// gate:allow code/config-literal reason: the public endpoint every installation uses\n\treturn get(\"https://api.shop.internal/v1/orders\")\n}\n\nfunc get(u string) string { return u }\n")
	markedTwo := client("", "func Fetch() string {\n\t// gate:allow code/config-literal reason: the public endpoint every installation uses\n\ta := get(\"https://api.shop.internal/v1/orders\")\n\n\treturn a + get(\"https://api.shop.internal/v1/items\")\n}\n\nfunc get(u string) string { return u }\n")
	return []engine.Case{
		{Name: "a URL added inside a function fails", Base: module(none), Head: module(one), WantFail: []string{"code/config-literal"}},
		{Name: "a path with no scheme passes", Base: module(client("")), Head: module(none)},
		{Name: "a namespace URL passes", Base: module(none), Head: module(namespace)},
		{Name: "a URL in the middle of a sentence passes", Base: module(none), Head: module(sentence)},
		{Name: "a URL added to a constant at the top of the file passes", Base: module(none), Head: module(constant)},
		{Name: "a URL added to a constant inside the function passes", Base: module(none), Head: module(local)},
		{Name: "a URL added in a file matching paths passes", Base: module(none, paths), Head: module(one, paths)},
		{Name: "a second URL added to the same function fails", Base: module(one), Head: module(client("", fetchTwo)),
			WantFail: []string{"code/config-literal"}},
		{Name: "editing the line with a URL passes when the number stays the same", Base: module(one), Head: module(edited)},
		{Name: "moving a function with a URL passes", Base: module(one), Head: module(moved)},
		{Name: "adding 30 * time.Second fails", Base: module(noTimeout), Head: module(timeout), WantFail: []string{"code/config-literal"}},
		{Name: "adding a host and port fails", Base: module(noAddr), Head: module(addr), WantFail: []string{"code/config-literal"}},
		{Name: "a literal given to a field names lists fails", Base: module(noPage, names), Head: module(page, names),
			WantFail: []string{"code/config-literal"}},
		{Name: "a literal given to a field names does not list passes", Base: module(noPage), Head: module(page)},
		{Name: "a URL in a test file passes", Base: module(client("")), Head: module(test)},
		{Name: "a marker with a reason sends the function to review", Base: module(none),
			Head:       module(client("", "func Fetch() string {\n\t// gate:allow code/config-literal reason: the public endpoint every installation uses\n\treturn get(\"https://api.shop.internal/v1/orders\")\n}\n\nfunc get(u string) string { return u }\n")),
			WantReview: []string{"exception:code/config-literal"}},
		{Name: "a marker covers only its own place: a URL added further down fails", Base: module(marked), Head: module(markedTwo),
			WantFail: []string{"code/config-literal"}},
		{Name: "adding a configuration path needs review", Base: module(none, map[string]string{".hadome.yml": "schema: 1\n"}),
			Head: module(none, paths), WantReview: []string{engine.LoosenedID}},
	}
}

const store = `type Store struct{}

func (Store) Save() error  { return nil }
func (Store) Count() (int, error) { return 0, nil }
func (Store) Name() string { return "" }
`

func swallowedErrorCases() []engine.Case {
	base := client("", store, "func Run(s Store) error {\n\treturn s.Save()\n}\n")
	dropped := client("", store, "func Run(s Store) error {\n\t_ = s.Save()\n\treturn nil\n}\n")
	droppedTwo := client("", store, "func Run(s Store) error {\n\t_ = s.Save()\n\t_, _ = s.Count()\n\treturn nil\n}\n")
	notError := client("", store, "func Run(s Store) error {\n\t_ = s.Name()\n\treturn s.Save()\n}\n")
	unknown := client("", store, "type saver interface{ Save() error }\n\nfunc Run(s saver) error {\n\t_ = s.Save()\n\treturn nil\n}\n")
	std := client("\t\"os\"\n", "func Clean(p string) {\n\t_ = os.Remove(p)\n}\n")
	noStd := client("\t\"os\"\n", "func Clean(p string) error {\n\treturn os.Remove(p)\n}\n")
	moved := client("", store, "func Pad() {}\n\nfunc Run(s Store) error {\n\t_ = s.Save()\n\treturn nil\n}\n")
	parse := client("\t\"strconv\"\n", "func Parse(s string) int {\n\tn, _ := strconv.Atoi(s)\n\treturn n\n}\n")
	noParse := client("\t\"strconv\"\n", "func Parse(s string) (int, error) {\n\treturn strconv.Atoi(s)\n}\n")
	return []engine.Case{
		{Name: "dropping a repository function's error into _ fails", Base: module(base), Head: module(dropped),
			WantFail: []string{"code/swallowed-error"}},
		{Name: "dropping a second error in the same function fails", Base: module(dropped), Head: module(droppedTwo),
			WantFail: []string{"code/swallowed-error"}},
		{Name: "dropping a result that is not an error into _ passes", Base: module(base), Head: module(notError)},
		{Name: "dropping the value of a call through an interface passes", Base: module(base), Head: module(unknown)},
		{Name: "dropping a standard library function's error fails", Base: module(noStd), Head: module(std),
			WantFail: []string{"code/swallowed-error"}},
		{Name: "n, _ := strconv.Atoi(s) fails", Base: module(noParse), Head: module(parse), WantFail: []string{"code/swallowed-error"}},
		{Name: "an existing dropped error that moved passes", Base: module(dropped), Head: module(moved)},
		{Name: "a marker with a reason sends the function to review", Base: module(noStd),
			Head:       module(client("\t\"os\"\n", "func Clean(p string) {\n\t// gate:allow code/swallowed-error reason: the file may already be gone\n\t_ = os.Remove(p)\n}\n")),
			WantReview: []string{"exception:code/swallowed-error"}},
	}
}

// roundTotal is a function long enough to be compared, with its name and its variable to fill in.
func roundTotal(name, v string) string {
	return "func " + name + "(" + v + " float64, digits int) float64 {\n" +
		"\tscale := 1.0\n\tfor i := 0; i < digits; i++ {\n\t\tscale *= 10\n\t}\n" +
		"\tif " + v + " < 0 {\n\t\treturn -float64(int(-" + v + "*scale+0.5)) / scale\n\t}\n" +
		"\treturn float64(int(" + v + "*scale+0.5)) / scale\n}\n"
}

// fixed is roundTotal with a fix: it rounds half to even.
func fixed(name string) string {
	return strings.Replace(roundTotal(name, "total"), "+0.5", "+0.4999", 2)
}

func duplicateCases() []engine.Case {
	billing := func(body string) string { return "package billing\n\n" + body }
	invoice := func(body string) string { return "package invoice\n\n" + body }
	one := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total"))}
	two := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundAgain", "sum"))}
	three := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundAgain", "sum") +
		"\n" + roundTotal("roundThird", "x"))}
	renamed := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundOnceMore", "sum"))}
	literal := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" +
		strings.Replace(roundTotal("roundAgain", "sum"), "10", "100", 1))}
	typed := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" +
		strings.Replace(roundTotal("roundAgain", "sum"), "digits int", "digits int64", 1))}
	twoFixed := map[string]string{"billing/round.go": billing(fixed("RoundTotal") + "\n" + strings.Replace(roundTotal("roundAgain", "sum"), "+0.5", "+0.4999", 2))}
	small := map[string]string{"billing/a.go": billing("func A(x int) int { return x + 1 }\n\nfunc B(y int) int { return y + 1 }\n")}
	otherPkg := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total")),
		"invoice/round.go": invoice(roundTotal("RoundTotal", "total"))}
	test := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total")),
		"billing/round_test.go": billing(roundTotal("roundInTest", "total"))}
	big := map[string]string{"billing/round.go": billing(roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundAgain", "sum")),
		".hadome.yml": "schema: 1\nrules:\n  code:\n    duplicate:\n      min-tokens: 500\n"}
	// osPair is a pair of copies in the Linux file and another in the Windows file, never built together.
	osPair := func(linux, windows string) map[string]string {
		return map[string]string{"billing/round_linux.go": billing(linux), "billing/round_windows.go": billing(windows)}
	}
	linux := roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundAgain", "sum")
	windows := roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundThird", "x")
	onOS := osPair(linux, windows)
	fixedOnLinux := osPair(fixed("RoundTotal")+"\n"+strings.Replace(roundTotal("roundAgain", "sum"), "+0.5", "+0.4999", 2), windows)
	alone := osPair(linux, roundTotal("RoundTotal", "total"))
	return []engine.Case{
		{Name: "fixing both copies in one operating system's file passes", Base: module(onOS), Head: module(fixedOnLinux)},
		{Name: "a copy added in one operating system's file fails though another file has a pair", Base: module(alone),
			Head: module(onOS), WantFail: []string{"code/duplicate"}},
		{Name: "adding a copy of an existing function fails", Base: module(one), Head: module(two), WantFail: []string{"code/duplicate"}},
		{Name: "a copy in another package with the same calls fails", Base: module(one), Head: module(otherPkg), WantFail: []string{"code/duplicate"}},
		{Name: "a function with one literal changed passes", Base: module(one), Head: module(literal)},
		{Name: "a function differing only in a parameter type passes", Base: module(one), Head: module(typed)},
		{Name: "copies smaller than min-tokens pass", Base: module(map[string]string{"billing/a.go": billing("")}), Head: module(small)},
		{Name: "a group of copies on base passes", Base: module(two), Head: module(two)},
		{Name: "a third copy added to a group fails", Base: module(two), Head: module(three), WantFail: []string{"code/duplicate"}},
		{Name: "renaming a copy passes", Base: module(two), Head: module(renamed)},
		{Name: "fixing every copy of a group the same way passes", Base: module(two), Head: module(twoFixed)},
		{Name: "a copy in a test file passes", Base: module(one), Head: module(test)},
		{Name: "a raised min-tokens needs review", Base: module(two, map[string]string{".hadome.yml": "schema: 1\n"}), Head: module(big),
			WantReview: []string{engine.LoosenedID}},
	}
}

func divergedCopyCases() []engine.Case {
	pair := func(a, b string) map[string]string {
		return map[string]string{"billing/round.go": "package billing\n\n" + a, "invoice/round.go": "package invoice\n\n" + b}
	}
	base := pair(roundTotal("RoundTotal", "total"), roundTotal("RoundTotal", "total"))
	oneFixed := pair(fixed("RoundTotal"), roundTotal("RoundTotal", "total"))
	bothFixed := pair(fixed("RoundTotal"), fixed("RoundTotal"))
	apart := pair(fixed("RoundTotal"), strings.Replace(roundTotal("RoundTotal", "total"), "+0.5", "+0.6", 2))
	deleted := map[string]string{"billing/round.go": "package billing\n\n" + roundTotal("RoundTotal", "total"), "invoice/round.go": "package invoice\n"}
	notCopies := pair(roundTotal("RoundTotal", "total"), strings.Replace(roundTotal("RoundTotal", "total"), "10", "100", 1))
	notCopiesFixed := pair(fixed("RoundTotal"), strings.Replace(roundTotal("RoundTotal", "total"), "10", "100", 1))
	excused := pair(fixed("RoundTotal"), "// gate:allow code/diverged-copy reason: invoices keep the rounding the tax office signed off\n"+
		roundTotal("RoundTotal", "total"))
	variants := map[string]string{
		"billing/round_linux.go":   "package billing\n\n" + roundTotal("RoundTotal", "total") + "\n" + roundTotal("roundAgain", "sum"),
		"billing/round_windows.go": "package billing\n\n" + strings.Replace(roundTotal("RoundTotal", "total"), "10", "100", 1)}
	variantGone := map[string]string{
		"billing/round_linux.go":   "package billing\n\n" + roundTotal("roundAgain", "sum"),
		"billing/round_windows.go": variants["billing/round_windows.go"]}
	return []engine.Case{
		{Name: "deleting a copy whose name another operating system's file also declares passes", Base: module(variants),
			Head: module(variantGone)},
		{Name: "fixing one copy reports the other", Base: module(base), Head: module(oneFixed), WantFail: []string{"code/diverged-copy"}},
		{Name: "fixing both copies the same way passes", Base: module(base), Head: module(bothFixed)},
		{Name: "changing both copies differently passes", Base: module(base), Head: module(apart)},
		{Name: "deleting one copy passes", Base: module(base), Head: module(deleted)},
		{Name: "functions that were not copies on base are not reported", Base: module(notCopies), Head: module(notCopiesFixed)},
		{Name: "a reasoned exception on the copy left behind needs review", Base: module(base), Head: module(excused),
			WantReview: []string{"exception:code/diverged-copy"}},
	}
}
