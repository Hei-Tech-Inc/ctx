# Security policy

## Reporting a vulnerability

Please report security issues **privately** so we can fix them before public disclosure.

- **Preferred:** GitHub [Security Advisories](https://github.com/Hei-Tech-Inc/ctx/security/advisories/new) for this repository (private report).
- **Alternative:** Email the repository maintainer if you cannot use GitHub.

Include:

- A clear description of the issue and impact
- Steps to reproduce (proof-of-concept if possible)
- Affected versions or install path (`main` vs tagged release)

We aim to acknowledge reports within a few business days and coordinate disclosure after a fix is available.

## Scope

This policy applies to the `ctx` CLI, `install.sh`, and supporting shell libraries in this repository.

Out of scope: vulnerabilities in third-party tools `install.sh` may install (`mise`, `gum`, `gh`, cloud CLIs, etc.) — report those to their respective projects.

## Installation note

Installing via `curl … | bash` trusts TLS to GitHub and executes `install.sh` from the checked-out ref. To pin trust to a specific release, install from a **tagged** URL (see `README.md` → pinned installs).
