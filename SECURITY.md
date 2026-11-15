# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately, not in a public issue. On GitHub, open the repository's **Security** tab and choose **Report a vulnerability**. This creates a private security advisory that only the maintainer can see.

Include what you found, how to reproduce it, and the kindtui version (`kindtui --version`). You will get a reply as soon as practical, and a fix will be released and credited in the advisory unless you ask otherwise.

## Scope

- The `kindtui` binary and its source in this repository
- Scripts, the Makefile, and the CI/release workflows in `.github/`

Bugs in `kind`, `kubectl`, `k9s`, Docker, or Podman themselves should go to those projects.

## Bug bounty

There is no bug bounty. This is a personal open-source project.
