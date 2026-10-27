# 0005: Dependencies before release

**Date:** 2026-10-27. **Status:** decided; see the outcome below and DL-280 in
[design.md](../design.md#decision-log).

This settles api.md's open item: that the libraries' licences and maintenance are confirmed before the first release.
Once the modules are released, each dependency is inherited by every app that uses them, so the decision is hard to
reverse (DL-255).

**Sources:** each dependency's `LICENSE` in the module cache, at the versions the released modules require, and the
upstream repositories' GitHub metadata, both read on 2026-10-27.

---

## The question

The released modules are the core, `format/toml`, `format/yaml`, `ociresolver`, `watch/fsnotify` and `presets`. Is every
third-party module they build with, direct or indirect, under a licence compatible with Apache-2.0? Is any of them at
risk of going unmaintained, and if so, what would replacing it cost?

## Licences

`go list -deps` over the released modules' packages, with `GOWORK=off`, finds nine third-party modules.

| Module                             | Version | Licence      | Used by                   |
| ---------------------------------- | ------- | ------------ | ------------------------- |
| `github.com/go-viper/mapstructure/v2` | v2.5.0 | MIT          | core                      |
| `github.com/pelletier/go-toml/v2`  | v2.4.3  | MIT          | `format/toml`             |
| `github.com/goccy/go-yaml`         | v1.19.2 | MIT          | `format/yaml`             |
| `github.com/fsnotify/fsnotify`     | v1.10.1 | BSD-3-Clause | `watch/fsnotify`          |
| `golang.org/x/sys`                 | v0.49.0 | BSD-3-Clause | `watch/fsnotify`          |
| `oras.land/oras-go/v2`             | v2.6.4  | Apache-2.0   | `ociresolver`             |
| `github.com/opencontainers/image-spec` | v1.1.1 | Apache-2.0 | `ociresolver`             |
| `github.com/opencontainers/go-digest` | v1.0.0 | Apache-2.0  | `ociresolver`             |
| `golang.org/x/sync`                | v0.24.0 | BSD-3-Clause | `ociresolver`             |

All three licences are permissive and compatible with Apache-2.0. None of the Apache-2.0 modules ships a `NOTICE` file,
so there is no notice to carry. The modules are distributed as source, so each dependency's licence travels with its own
module.

## Maintenance

| Upstream                   | Archived | Last push  | Latest release        | Open issues |
| -------------------------- | -------- | ---------- | --------------------- | ----------: |
| go-viper/mapstructure      | no       | 2026-05-04 | v2.5.0, 2026-01-12    |          54 |
| pelletier/go-toml          | no       | 2026-08-03 | v2.4.3, 2026-07-05    |          45 |
| goccy/go-yaml              | no       | 2026-04-11 | v1.19.2, 2026-01-08   |         260 |
| fsnotify/fsnotify          | no       | 2026-05-11 | v1.10.1, 2026-05-04   |          67 |
| oras-project/oras-go       | no       | 2026-10-26 | v2.7.0, 2026-10-23    |           2 |
| opencontainers/image-spec  | no       | 2026-10-22 | v1.1.1, 2025-03-03    |          75 |
| opencontainers/go-digest   | no       | 2026-10-15 | v1.0.0, 2020-05-14    |          11 |

Every module is at its upstream's latest release except oras-go, whose v2.7.0 is four days old. go-digest's last release
is old because its API is complete; its repository is still maintained.

goccy/go-yaml shows the weakest signals: no push for six months, no release for nine, and the most open issues.
[Conformance](../../conformance.md) already records its known failures against yaml-test-suite.

## Options

1. **Keep every dependency, and record each one's exit (recommended).** Each dependency is behind a boundary of our own,
   so replacing one is a release of one module, not a breaking change for apps:
   - goccy/go-yaml and go-toml are reached only through `format/yaml` and `format/toml`, behind the `Decoder` interface.
     A replacement is measured by the same conformance suites before it ships.
   - mapstructure has been out of the API since DL-278, so the core can swap it without breaking anything.
   - fsnotify is behind `Notifier`, and oras-go behind `IncludeResolver`.
2. **Replace goccy/go-yaml now.** This would trade a known set of failures, already measured and documented, for an
   unmeasured library just before the first release. The boundary makes it just as cheap to do later, if the signals get
   worse.

## Outcome

**Option 1**, decided on 2026-10-27 and recorded as DL-280: every dependency stays for v0.1.0. Their licences and
maintenance are checked again before each release. oras-go moves to v2.7.0 once it's two weeks old and its changelog
shows no break. A dependency added later fails the dependency check until its licence is reviewed and listed.
