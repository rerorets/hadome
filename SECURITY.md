# Security

## Reporting a vulnerability

Do not open an issue for a vulnerability. Report it privately through GitHub:

1. Open the report form: https://github.com/rerorets/hadome/security/advisories/new
2. Write how to reproduce it and which versions it affects

The form also opens from "Report a vulnerability" on the repository's "Security" tab.

Only the maintainer reads the report. Its content stays private until a fixed version is released.

## Supported versions

Only the latest version gets fixes. If you use an older one, upgrade.

## What hadome runs

hadome only reads the repository it is pointed at and never talks to the network. It does run the external commands listed under `gates` in the configuration, with the same permissions as the repository's other scripts ([product-specific gates](docs/external-gates.md)). Before running hadome on a repository you do not trust, read the `gates` in its `.hadome.yml`.
