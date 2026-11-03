package gate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"

	"github.com/rerorets/hadome/internal/engine"
)

// exec runs the command in dir with the gate's arguments and extra ones, feeding stdin. Standard
// error goes to the diagnostics, prefixed by the gate's ID.
func (r *runner) exec(stdin []byte, dir string, extra ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), r.timeout)
	defer cancel()
	args := append(append([]string{}, r.gate.Run[1:]...), extra...)
	cmd := exec.CommandContext(ctx, r.command, args...)
	cmd.Dir = dir
	cmd.Stdin = bytes.NewReader(stdin)
	var stdout bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &prefixed{w: r.diag, prefix: r.gate.ID + ": "}
	err := cmd.Run()
	switch {
	case errors.Is(ctx.Err(), context.DeadlineExceeded):
		return stdout.Bytes(), fmt.Errorf("did not finish within %s", r.timeout)
	case err != nil:
		return stdout.Bytes(), err
	}
	return stdout.Bytes(), nil
}

// call sends a request about one version and decodes the answer into out, which must state the
// protocol's schema version.
func (r *runner) call(req request, out interface{ schema() int }) error {
	in, err := json.Marshal(req)
	if err != nil {
		return err
	}
	stdout, err := r.exec(in, req.Version.Root)
	if err != nil {
		return fmt.Errorf("gate %s on %s: %w", r.gate.ID, req.Version.Label, err)
	}
	dec := json.NewDecoder(bytes.NewReader(stdout))
	dec.DisallowUnknownFields()
	if err := dec.Decode(out); err != nil {
		return fmt.Errorf("gate %s on %s: the answer is not the protocol's JSON: %w", r.gate.ID, req.Version.Label, err)
	}
	if out.schema() != protocolVersion {
		return fmt.Errorf("gate %s on %s: answer has schema %d, want %d", r.gate.ID, req.Version.Label, out.schema(), protocolVersion)
	}
	return nil
}

func (r *runner) ask(v engine.Version, out interface{ schema() int }) error {
	req, err := r.newRequest(v)
	if err != nil {
		return err
	}
	return r.call(req, out)
}

// prefixed writes each line with a prefix, so diagnostics show which gate said them.
type prefixed struct {
	w      io.Writer
	prefix string
	mid    bool // in the middle of a line
}

func (p *prefixed) Write(b []byte) (int, error) {
	for _, c := range b {
		if !p.mid {
			if _, err := io.WriteString(p.w, p.prefix); err != nil {
				return 0, err
			}
		}
		if _, err := p.w.Write([]byte{c}); err != nil {
			return 0, err
		}
		p.mid = c != '\n'
	}
	return len(b), nil
}
