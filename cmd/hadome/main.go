// Command hadome stops regressions in code and tests, not progress.
package main

import (
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"

	"github.com/rerorets/hadome/internal/adapter"
	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
	"github.com/rerorets/hadome/internal/gate"
	"github.com/rerorets/hadome/internal/mutation"
	"github.com/rerorets/hadome/internal/rules/comments"
	"github.com/rerorets/hadome/internal/rules/deps"
	"github.com/rerorets/hadome/internal/rules/size"
	"github.com/rerorets/hadome/internal/rules/tests"
	"github.com/rerorets/hadome/internal/rules/unknown"
	"github.com/rerorets/hadome/internal/source"
	"github.com/rerorets/hadome/internal/suggest"
)

// version is set when a release is built: go build -ldflags "-X main.version=1.2.0".
var version = "dev"

// modulePath is hadome's module, whose version Go embeds when it builds hadome from a module.
const modulePath = "github.com/rerorets/hadome"

// versionOf is the version hadome prints: the stamped one, or else the module version Go embedded,
// since go get -tool builds from source without the release's ldflags.
func versionOf(stamped string, info *debug.BuildInfo, ok bool) string {
	if stamped != "dev" || !ok || info.Main.Path != modulePath {
		return stamped
	}
	switch v := info.Main.Version; v {
	case "", "(devel)":
		return stamped
	default:
		return strings.TrimPrefix(v, "v")
	}
}

func main() { os.Exit(run(os.Args[1:], os.Stdout, os.Stderr)) }

func newEngine() *engine.Engine {
	var rules []engine.Rule
	for _, group := range [][]engine.Rule{tests.All(), size.All(), comments.All(), deps.All(), unknown.All()} {
		rules = append(rules, group...)
	}
	return &engine.Engine{Analyzer: adapter.New(), Rules: rules, Workdir: ".", Candidates: suggest.Candidates,
		Elsewhere: func(rule string) bool { return rule == mutation.RuleID }}
}

func run(args []string, stdout, stderr io.Writer) int {
	if len(args) == 0 {
		fmt.Fprint(stderr, usage)
		return engine.ExitConfig
	}
	switch args[0] {
	case "check":
		return check(args[1:], stdout, stderr)
	case "mutate":
		return mutate(args[1:], stdout, stderr)
	case "self-test":
		return selfTest(stdout, stderr)
	case "config":
		return configCmd(args[1:], stdout, stderr)
	case "schema":
		_, _ = stdout.Write(config.JSONSchema())
		return engine.ExitOK
	case "explain":
		return explain(args[1:], stdout, stderr)
	case "suggest":
		return suggestCmd(args[1:], stdout, stderr)
	case "agents":
		return agents(args[1:], stdout, stderr)
	case "init":
		return initCmd(args[1:], stdout, stderr)
	case "version", "--version":
		info, ok := debug.ReadBuildInfo()
		fmt.Fprintln(stdout, "hadome", versionOf(version, info, ok))
		return engine.ExitOK
	default:
		fmt.Fprintf(stderr, "hadome: unknown command %q\n", args[0])
		return engine.ExitConfig
	}
}

func check(args []string, stdout, stderr io.Writer) int {
	fl := flag.NewFlagSet("check", flag.ContinueOnError)
	fl.SetOutput(stderr)
	base := fl.String("base", "", "base revision (default: origin/main, else main)")
	head := fl.String("head", "", "head revision (default: the working tree)")
	format := fl.String("format", "text", "output format: text or json")
	baseDir := fl.String("base-dir", "", "compare two directories instead of git: base directory")
	headDir := fl.String("head-dir", "", "compare two directories instead of git: head directory")
	lang := fl.String("lang", "", "language of the findings' wording and the summary: en or ja (default: messages in .hadome.yml, else en)")
	if err := fl.Parse(args); err != nil || (*lang != "" && !validLang(*lang, stderr)) {
		return engine.ExitConfig
	}
	src, ok := pickSource(*base, *head, *baseDir, *headDir, stderr)
	if !ok {
		return engine.ExitConfig
	}
	e := newEngine()
	var copies []io.Closer
	defer func() {
		for _, c := range copies {
			_ = c.Close()
		}
	}()
	e.ConfigRules = func(c config.Config) ([]engine.Rule, error) {
		rules, closer, err := gate.Rules(c, ".", stderr)
		if closer != nil {
			copies = append(copies, closer)
		}
		return rules, err
	}
	r, err := e.Check(src)
	if err != nil {
		fmt.Fprintln(stderr, "hadome: the check could not run, so it does not pass:", err)
		return engine.ExitConfig
	}
	return writeReport(stdout, r, *format, pickLang(*lang, r.Messages))
}

// writeReport writes the report of check or mutate as JSON or text, its wording in lang.
func writeReport(stdout io.Writer, r engine.Report, format, lang string) int {
	if format == "json" {
		if err := engine.WriteJSON(stdout, r, lang); err != nil {
			return engine.ExitConfig
		}
	} else {
		engine.WriteText(stdout, r, lang)
	}
	return r.ExitCode()
}

// pickSource names the two versions: two revisions, or two directories, never both.
func pickSource(base, head, baseDir, headDir string, stderr io.Writer) (engine.Source, bool) {
	switch {
	case (baseDir != "" || headDir != "") && (base != "" || head != ""):
		fmt.Fprintln(stderr, "hadome: compare either two revisions (--base, --head) or two directories (--base-dir, --head-dir), not both")
		return nil, false
	case baseDir != "" || headDir != "":
		return source.Dirs{BaseDir: baseDir, HeadDir: headDir}, true
	}
	if base == "" {
		base = defaultBase()
	}
	return source.Git{Root: ".", BaseRev: base, HeadRev: head}, true
}

func defaultBase() string {
	for _, rev := range []string{"origin/main", "main"} {
		if exec.Command("git", "rev-parse", "--verify", "--quiet", rev).Run() == nil {
			return rev
		}
	}
	return "HEAD"
}

func selfTest(stdout, stderr io.Writer) int {
	e := newEngine()
	c, err := config.Load(os.DirFS("."), ".")
	if err != nil {
		fmt.Fprintln(stderr, err)
		return engine.ExitConfig
	}
	gates, closer, err := gate.Rules(c, ".", stderr)
	if err != nil {
		fmt.Fprintln(stderr, "hadome:", err)
		return engine.ExitConfig
	}
	defer closer.Close()
	e.Rules = append(e.Rules, gates...)
	failed := e.SelfTest()
	for _, f := range failed {
		fmt.Fprintf(stdout, "FAIL %s: %s: %s\n", f.Rule, f.Name, f.Err)
	}
	if len(failed) > 0 {
		return 1
	}
	var ids []string
	for _, r := range e.Rules {
		ids = append(ids, r.ID())
	}
	fmt.Fprintln(stdout, "ok: every rule's self-test cases pass ("+strings.Join(ids, ", ")+")")
	return 0
}
