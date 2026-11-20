# Security

## Reporting a vulnerability

Do not open an issue for a vulnerability. Report it privately through GitHub:

1. Open the report form: https://github.com/rerorets/hadome/security/advisories/new
2. Write how to reproduce it and which versions it affects

The form also opens from "Report a vulnerability" on the repository's "Security" tab.

Only the maintainer reads the report. Its content stays private until a fixed version is released.

## After a report

1. Within 14 days, the maintainer replies that the report was received
2. The maintainer reproduces it and decides which versions it affects and how severe it is
3. A fixed version is released, and a GitHub security advisory is published, with a CVE when needed
4. The advisory and the [changelog](CHANGELOG.md) credit the reporter by name. If you would rather not be named, say so in the report

When step 2 is done, the reporter is told how long the fix is expected to take.

## Supported versions

Only the latest version gets fixes. If you use an older one, upgrade.

## What hadome runs

hadome only reads the repository it is pointed at and never talks to the network. Two things do run the repository's code or commands:

- The external commands listed under `gates` in the configuration run with the same permissions as the repository's other scripts. `hadome check` runs only the gates the base branch's configuration declares with the same `id` and the same `run`. A gate a pull request adds, or whose command it changes, is not run and goes to a person's review with exit code `3`.
- `hadome mutate` runs the repository's test command. It runs it in a copy of the version in a working directory, and never rewrites the repository's working tree.

How to write the external commands is in [product-specific gates](docs/external-gates.md). Before running hadome on a repository you do not trust, read the `gates` in its `.hadome.yml` and any changes to the gates' scripts. When a pull request rewrites the script of a gate on the base branch, the rewritten script runs. Do not run `hadome mutate` on a repository you do not trust.

What hadome protects, and how, is in [the security assurance case](docs/assurance-case.md).
