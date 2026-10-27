# Changelog: `github.com/kittomic/cascadingconfig/format/toml`

All notable changes to this module. Entries are built from [Conventional Commits](https://www.conventionalcommits.org/)
whose scope is `format/toml`, and each release is tagged `format/toml/vX.Y.Z`.

## v0.1.0

The first release: the TOML decoder, built on go-toml v2.4.3, with duplicate keys as errors and dates and times as
strings in the tree. Measured against toml-test in docs/conformance.md.
