# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately, through GitHub's
[private vulnerability reporting](https://github.com/kittomic/cascadingconfig/security/advisories/new). Don't open a
public issue.

Include what you found, how to reproduce it, and which module and version are affected. You'll get an acknowledgement,
and a fix is coordinated with you before anything is disclosed.

## Supported versions

Until 1.0, only the latest release of each module gets security fixes.

## Scope

The loader reads configuration that may come from untrusted places, such as a cloned repository or a remote include.
Bypasses of its protections are in scope, for example:

- path traversal or symlink escapes, including through includes and environment names
- include allow-list, pinning or trust-store bypasses
- archive or redirect tricks in the `http`, `git` and `oci` resolvers
- inputs that hang the loader or make it use unbounded memory

See [Traversal protection](docs/design.md#traversal-protection) for what the loader is designed to prevent.
