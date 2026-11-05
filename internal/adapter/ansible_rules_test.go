package adapter_test

import (
	"slices"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/rerorets/hadome/internal/engine"
)

const (
	webTasks    = "- name: Install the server\n  ansible.builtin.package:\n    name: nginx\n"
	webDefaults = "web_port: 80\n"
	webSpecs    = "argument_specs:\n  main:\n    options:\n      web_port:\n        description: The port the server listens on.\n"
	converge    = "- hosts: all\n  roles:\n    - web\n"
	verified    = "- hosts: all\n  tasks:\n    - ansible.builtin.assert:\n        that: ansible_facts.packages.nginx is defined\n"
)

// role is the Ansible role web with these defaults and, unless verify is empty, a Molecule scenario
// that applies the role through conv and checks it with verify.
func role(defaults, conv, verify string) fstest.MapFS {
	t := fstest.MapFS{
		"roles/web/tasks/main.yml":          {Data: []byte(webTasks)},
		"roles/web/defaults/main.yml":       {Data: []byte(webDefaults + defaults)},
		"roles/web/meta/argument_specs.yml": {Data: []byte(webSpecs)},
	}
	if verify != "" {
		t["roles/web/molecule/default/converge.yml"] = &fstest.MapFile{Data: []byte(conv)}
		t["roles/web/molecule/default/verify.yml"] = &fstest.MapFile{Data: []byte(verify)}
	}
	return t
}

// viewpoint: external contract
// target: adapter.Analyzer through engine.Engine.Check
// Ansible has no skips and no mocks, but every fact it does have reaches the same rules as in the
// other languages, in builds with and without cgo.
// guards: same-rules-every-language
func TestTheSameRegressionFailsInAnsible(t *testing.T) {
	base := role("", converge, verified)
	withTasks := func(tasks string) fstest.MapFS {
		r := role("", converge, verified)
		r["roles/web/tasks/main.yml"] = &fstest.MapFile{Data: []byte(webTasks + tasks)}
		return r
	}
	cases := []struct {
		name, want string
		base, head fstest.MapFS
	}{
		{"deleting the only scenario", "tests/scope-shrunk", base, role("", "", "")},
		{"removing its assert", "tests/no-assertion", base,
			role("", converge, "- hosts: all\n  tasks:\n    - ansible.builtin.package_facts:\n")},
		{"applying no role of the repository", "tests/guards-nothing", base,
			role("", "- hosts: all\n  roles:\n    - community.general.ufw\n", verified)},
		{"waiting with pause", "tests/no-sleep", base,
			role("", converge, verified+"    - ansible.builtin.pause:\n        seconds: 5\n")},
		{"adding a default argument_specs does not describe", "comments/missing-doc", base, role("web_user: www\n", converge, verified)},
		{"adding one kept to the role", "", base, role("_web_cache: /tmp\n", converge, verified)},
		{"breaking the YAML", "unknown/unread", base, role("web_user: [\n", converge, verified)},
		{"including a role named by a template", "unknown/import", base,
			withTasks("- ansible.builtin.include_role:\n    name: \"{{ extra_role }}\"\n")},
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
// A playbook outside Ansible's layout is read once files.ansible names it, and dropping the entry is
// a loosening a person reviews: the file would leave every rule.
func TestFilesAnsibleAddsPlaybooks(t *testing.T) {
	with := func(cfg, playbook string) fstest.MapFS {
		r := role("", converge, verified)
		r["deploy/web.yml"] = &fstest.MapFile{Data: []byte(playbook)}
		if cfg != "" {
			r[".hadome.yml"] = &fstest.MapFile{Data: []byte("schema: 1\n" + cfg)}
		}
		return r
	}
	listed := "files:\n  ansible: [\"deploy/*.yml\"]\n"
	const plain = "- hosts: web\n  roles:\n    - web\n"
	const templated = "- hosts: web\n  roles:\n    - \"{{ extra_role }}\"\n"
	if got := regressions(t, with(listed, plain), with(listed, templated)); !slices.Contains(got, "unknown/import") {
		t.Errorf("a listed playbook: got %v, want unknown/import among them", got)
	}
	if got := regressions(t, with("", plain), with("", templated)); len(got) > 0 {
		t.Errorf("an unlisted playbook is read: %v", got)
	}
	if got := regressions(t, with(listed, plain), with("files:\n  ansible: []\n", plain)); strings.Join(got, ",") != engine.LoosenedID {
		t.Errorf("removing the entry: got %v, want [%s]", got, engine.LoosenedID)
	}
	if got := regressions(t, with("", plain), with(listed, plain)); len(got) > 0 {
		t.Errorf("adding the entry: got %v, want nothing", got)
	}
}
