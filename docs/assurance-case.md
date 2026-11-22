# Security assurance case

What hadome protects, and what it does to protect it. How to report a vulnerability is in [Security](../SECURITY.md).

## What is protected

| What | What happens if it is harmed |
|---|---|
| The user's machine and CI | Merely running hadome runs an unintended command, rewrites files or sends information out |
| The result of the check | Broken input lets a change through that should be stopped, or makes hadome crash |
| The binaries hadome ships | A binary replaced on the way runs on the user's machine |

## Trust boundaries

hadome does not trust the contents of the repository it reads. Whoever made the change, that version's files, configuration and tests are input from outside.

```mermaid
flowchart LR
  subgraph untrusted[Not trusted: the repository read]
    src[Source and tests]
    cfg[.hadome.yml]
  end
  subgraph hadome[The hadome process]
    read[Readers: the Go parser and tree-sitter]
    rules[Rules and comparison]
  end
  subgraph runs[What runs the repository's commands]
    gates[gates in the configuration]
    mutate[the tests of hadome mutate]
  end
  src --> read --> rules
  cfg --> rules
  cfg -.-> gates
  cfg -.-> mutate
  rules --> out[Findings and exit code]
```

- **The part that only reads.** `hadome check` reads files and parses them. The only repository code it runs is the `gates` in the base branch's configuration
- **What is read outside the repository.** Only the shared configuration files base's configuration lists in `extends`. A path head adds is not read ([configuration design](design/configuration.md#reading-the-shared-layers))
- **The parts that run commands.** `gates` in the configuration and `hadome mutate` run the repository's commands. They are treated as trusted to the same degree as the repository's other scripts. On a repository you do not trust, read its `gates` first, and do not run `hadome mutate`
- **The gates `hadome check` runs.** Only those the base branch's configuration declares with the same `id` and the same `run`. A gate the change adds, or whose command it changes, is not run, not even its self-test, and goes to a person's review with exit code `3` ([gates design](design/gates.md)). The `.hadome.yml` in a change cannot run a new command on its own

## Threats and countermeasures

| Threat | Countermeasure |
|---|---|
| A crafted file breaks the parsing | The parts written in Go are memory-safe. The tree-sitter grammars, written in C, are fuzzed with AddressSanitizer in CI |
| A crafted configuration or test never finishes | The commands of `gates` and the test command of `hadome mutate` have a time limit. A test command over its limit is ended together with its whole process tree ([mutation design](design/mutation.md)) |
| A symbolic link makes hadome write outside the repository | `check` and `mutate` never rewrite the repository's working tree. Only `hadome init` and `hadome agents --write` write to it, and neither writes outside it through a symbolic link: init writes no path that exists, and agents --write writes through a link only to a regular file inside the repository and stops when given any other link. If the temporary directory of `hadome mutate` is a symbolic link or belongs to another user, hadome stops with exit code `2` without writing to it |
| A symbolic link makes hadome read a file outside the repository | hadome never follows a symbolic link in the repository. As git stores one, it reads the link as a file whose content is the path it points to, in a revision, the working tree and the directories of `--base-dir` and `--head-dir` alike ([engine design](design/engine.md#symbolic-links-and-special-files)) |
| `extends` makes hadome read a file on the CI machine | Base's configuration decides which `extends` a check reads. A change to `extends` in head goes to a person's review (exit code `3`), and the files it adds are not read. Validation errors in shared layers show no values |
| A crafted file name swaps in another file's content | git is given object IDs only, never paths, and each answer is checked to be the object asked for. A missing answer stops hadome with exit code `2` instead of being skipped |
| An input without end exhausts memory or hangs hadome | hadome reads regular files only, up to 128 MiB each, and configuration files up to 1 MiB. A file over 128 MiB is not read and goes to a person's review with exit code `3`. A configuration file over its limit stops hadome with exit code `2`. FIFOs and devices are not read |
| Information is sent out, or configuration is fetched, over the network | hadome does not talk to the network. That it never reads configuration from the network is one of hadome's own invariants (`no-network` in `.hadome.yml`) |
| A crafted file name or exception reason that slips a forged display or command into the terminal or the CI log | The `text` output writes a value with a control character quoted and escaped. In `json`, `encoding/json` escapes it. [The CI example](ci.md) escapes the values it puts into warning commands and table cells |
| Loosening the configuration to slip past the check | Exceptions and changes that loosen the configuration go to a person's review with exit code `3`. An exception without a reason has no effect |
| Adding a gate to a pull request's `.hadome.yml` to run a command in CI | `hadome check` runs only the gates the base branch's configuration declares with the same `id` and the same `run`. Any other gate is not run and goes to a person's review as `config/gate-not-run` with exit code `3`. A change that rewrites the script of a gate already on the base branch is not prevented: that script runs with the same trust as the repository's other scripts |
| The shipped binaries are replaced | Releases are built only by GitHub Actions, with SHA256SUMS and minisign signatures. npm, PyPI and crates.io packages are published through trusted publishing, with no tokens stored. The workflows' actions are pinned to commit SHAs |

## Design principles

- **Least privilege.** Workflow tokens are read-only by default. Only the jobs that publish get the permissions they need. Publishing credentials are kept in environments that only version tags can use
- **Fail safe.** What hadome could not try or could not identify is not let through; it goes to a person's review (exit code `3`)
- **Economy of mechanism.** One Go binary that fetches nothing at run time
- **Input validation.** `.hadome.yml` is checked against its schema when read; unknown keys and values of the wrong type stop hadome with exit code `2`

## Common weaknesses

| Weakness | Countermeasure |
|---|---|
| Memory corruption (CWE-119 and others) | The language protects the Go parts. The C grammars are checked by fuzzing with AddressSanitizer |
| Command injection (CWE-78) | hadome runs only the commands written in the configuration, and git. The gate commands `hadome check` runs are only those in the base branch's configuration. Commands do not go through a shell; they run as lists of arguments. Test names put in through `{tests}` and the like are each passed as one argument |
| Argument injection (CWE-88) | Every revision given to git comes after `--end-of-options`, so one starting with `-` is never read as an option. Paths are given to git neither as arguments nor in newline-separated input |
| Path traversal and link following (CWE-22, CWE-59) | hadome writes only to its own working directories and to the files `init` and `agents --write` write, never outside the repository through a symbolic link, and never to another user's directory. When reading, it never follows a symbolic link in the repository |
| Resource exhaustion (CWE-400) | What hadome reads of one file is limited (128 MiB, 1 MiB for configuration files), and it reads regular files only |
| Leaked secrets (CWE-798) | No credentials are kept in the repository. GitHub secret scanning and push protection are on |

## How this is checked

- CodeQL analyses the Go code, the Actions workflows and the rest on every pull request and push to main
- OpenSSF Scorecard checks the repository's setup every week
- Dependabot reports vulnerable dependencies, and dependencies are updated every week
- [Security reviews](security-review.md) check that the claims in this document match the code
