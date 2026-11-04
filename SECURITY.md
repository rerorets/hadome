# Security

## Reporting a vulnerability

Do not open an issue for a vulnerability. Report it privately through GitHub:

1. Open the repository's "Security" tab
2. Press "Report a vulnerability"
3. Write how to reproduce it and which versions it affects

Only the maintainer reads the report. Its content stays private until a fixed version is released.

## Supported versions

Only the latest version gets fixes. If you use an older one, upgrade.

## What hadome runs

hadome only reads the repository it is pointed at and never talks to the network. It does run the external commands listed under `gates` in the configuration, with the same permissions as the repository's other scripts ([product-specific gates](docs/external-gates.md)). Before running hadome on a repository you do not trust, read the `gates` in its `.hadome.yml`.
