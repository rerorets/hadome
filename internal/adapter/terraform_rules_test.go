package adapter_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/rules/comments"
	"github.com/rerorets/hadome/internal/rules/size"
	"github.com/rerorets/hadome/internal/rules/tests"
	"github.com/rerorets/hadome/internal/rules/unknown"
)

type versions struct{ base, head fstest.MapFS }

func (s versions) Base() (engine.Version, error) {
	return engine.Version{Label: "base", FS: s.base}, nil
}
func (s versions) Head() (engine.Version, error) {
	return engine.Version{Label: "head", FS: s.head}, nil
}
func (s versions) Renames() (map[string]string, error) { return nil, nil }

const network = `variable "cidr" {
  description = "The network's address range."
}

output "id" {
  description = "The network's ID."
  value       = var.cidr
}
`

// module is a Terraform module with a test file holding one run block (none when run is empty).
// extra is added to the module's configuration.
func module(extra, run string) fstest.MapFS {
	t := fstest.MapFS{"modules/network/main.tf": {Data: []byte(network + extra)}}
	if run != "" {
		t["modules/network/tests/main.tftest.hcl"] = &fstest.MapFile{Data: []byte(run)}
	}
	return t
}

const checked = "run \"plans\" {\n  variables {\n    cidr = \"10.0.0.0/24\"\n  }\n  assert {\n    condition     = output.id != \"\"\n    error_message = \"no id\"\n  }\n}\n"

const provider = "\nterraform {\n  required_providers {\n    aws = {\n      source = \"hashicorp/aws\"\n    }\n  }\n}\n"

func regressions(t *testing.T, base, head fstest.MapFS) []string {
	t.Helper()
	rules := slices.Concat(tests.All(), comments.All(), size.All(), unknown.All())
	e := &engine.Engine{Analyzer: adapter.New(), Rules: rules}
	r, err := e.Check(versions{base: base, head: head})
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, f := range r.Regressions {
		got = append(got, f.Rule)
	}
	for _, f := range r.Reviews {
		got = append(got, f.Rule)
	}
	return got
}

// viewpoint: external contract
// target: adapter.Analyzer through engine.Engine.Check
// Terraform has no skips and no sleeps, but every fact it does have reaches the same rules as in the
// other languages, in builds with and without cgo.
// guards: same-rules-every-language
func TestTheSameRegressionFailsInTerraform(t *testing.T) {
	base := module("", checked)
	cases := []struct {
		name, want string
		base, head fstest.MapFS
	}{
		{"deleting the only test", "tests/scope-shrunk", base, module("", "")},
		{"removing its assert", "tests/no-assertion", base,
			module("", "run \"plans\" {\n  variables {\n    cidr = \"10.0.0.0/24\"\n  }\n}\n")},
		{"making it pass and read nothing of the module", "tests/guards-nothing", base,
			module("", "run \"plans\" {\n  assert {\n    condition     = 1 == 1\n    error_message = \"no\"\n  }\n}\n")},
		{"mocking a provider for a module that reaches nothing outside", "tests/no-mock", base,
			module("", "mock_provider \"aws\" {}\n\n"+checked)},
		{"the same mock for a module that requires a provider", "", module(provider, checked),
			module(provider, "mock_provider \"aws\" {}\n\n"+checked)},
		{"adding a variable without a description", "comments/missing-doc", base, module("\nvariable \"name\" {}\n", checked)},
		{"adding a variable with a description", "", base,
			module("\nvariable \"name\" {\n  description = \"The network's name.\"\n}\n", checked)},
		{"breaking the configuration", "unknown/unread", base, module("\nvariable \"name\" {\n", checked)},
		{"changing nothing", "", base, base},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := regressions(t, tc.base, tc.head)
			if (tc.want == "" && len(got) > 0) || !strings.Contains(strings.Join(got, ","), tc.want) {
				t.Errorf("want %q among the findings, got %v", tc.want, got)
			}
		})
	}
}

// viewpoint: behaviour
// target: adapter.Analyzer through engine.Engine.Check
// Rules that measure functions see none in Terraform: a variable longer than any function may be is
// not a long function.
func TestFunctionRulesSkipTerraformValues(t *testing.T) {
	long := "\nvariable \"zones\" {\n  description = \"Every zone.\"\n  default = [\n" +
		strings.Repeat("    \"zone\",\n", 100) + "  ]\n}\n"
	got := regressions(t, module("", checked), module(long, checked))
	if len(got) > 0 {
		t.Errorf("a 105-line variable is reported: %v", got)
	}
}
