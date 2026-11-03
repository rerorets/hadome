// Package gate runs product-specific gates: external commands declared in configuration, each
// turned into a rule that the engine runs and compares like a built-in one.
//
// Design: docs/design/gates.md · Protocol: docs/external-gates.md
package gate

import (
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"time"

	"github.com/rerorets/hadome/internal/config"
	"github.com/rerorets/hadome/internal/engine"
)

const (
	protocolVersion = 1
	defaultTimeout  = 60 * time.Second
)

// Rules builds one rule per gate of the configuration. Commands given as paths are found from
// workdir; what the commands print on standard error goes to diag. Close the returned closer when
// the check is over: it removes the copies of versions the gates were given.
func Rules(c config.Config, workdir string, diag io.Writer) ([]engine.Rule, io.Closer, error) {
	ws := &workspace{dirs: map[string]string{}}
	var out []engine.Rule
	for _, g := range c.Gates {
		r, err := newRunner(g, workdir, diag, ws)
		if err != nil {
			return nil, nil, fmt.Errorf("gate %s: %w", g.ID, err)
		}
		if g.Kind == "measure" {
			out = append(out, &measureGate{runner: r, items: map[string][]engine.Item{}})
		} else {
			out = append(out, &findingGate{runner: r})
		}
	}
	return out, ws, nil
}

// runner runs one gate's command.
type runner struct {
	gate    config.Gate
	command string // resolved run[0]
	workdir string
	timeout time.Duration
	diag    io.Writer
	ws      *workspace
}

func newRunner(g config.Gate, workdir string, diag io.Writer, ws *workspace) (*runner, error) {
	r := &runner{gate: g, command: g.Run[0], workdir: workdir, timeout: defaultTimeout, diag: diag, ws: ws}
	if g.Timeout != "" {
		d, err := time.ParseDuration(g.Timeout)
		if err != nil {
			return nil, err
		}
		r.timeout = d
	}
	if filepath.Base(r.command) != r.command {
		abs, err := filepath.Abs(filepath.Join(workdir, filepath.FromSlash(r.command)))
		if err != nil {
			return nil, err
		}
		r.command = abs
	}
	return r, nil
}

func (r *runner) id() string { return r.gate.ID }

// request is what a gate reads on standard input.
type request struct {
	Schema  int       `json:"schema"`
	Gate    string    `json:"gate"`
	Mode    string    `json:"mode,omitempty"`
	Version version   `json:"version"`
	Files   *[]string `json:"files,omitempty"` // nil only when asking about removed items
	Keys    []string  `json:"keys,omitempty"`  // only when asking about removed items
	Options any       `json:"options,omitempty"`
}

type version struct {
	Label string `json:"label"`
	Root  string `json:"root"`
}

// newRequest describes one version to the gate, with the files its paths select.
func (r *runner) newRequest(v engine.Version) (request, error) {
	root, err := r.ws.root(v)
	if err != nil {
		return request{}, err
	}
	files, err := selectFiles(v, r.gate.Paths)
	if err != nil {
		return request{}, err
	}
	sort.Strings(files)
	if files == nil {
		files = []string{}
	}
	return request{Schema: protocolVersion, Gate: r.gate.ID, Version: version{Label: v.Label, Root: root},
		Files: &files, Options: r.gate.Options}, nil
}

// Cases is empty: a gate's cases live with its command, which SelfTest runs.
func (r *runner) Cases() []engine.Case { return nil }

// SelfTest runs the command's own self-test from workdir.
func (r *runner) SelfTest() error {
	out, err := r.exec(nil, r.workdir, "--self-test")
	if err != nil {
		return fmt.Errorf("%w\n%s", err, out)
	}
	return nil
}
