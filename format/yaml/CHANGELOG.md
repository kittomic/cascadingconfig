# Changelog: `github.com/kittomic/cascadingconfig/format/yaml`

All notable changes to this module. Entries are built from [Conventional Commits](https://www.conventionalcommits.org/)
whose scope is `format/yaml`, and each release is tagged `format/yaml/vX.Y.Z`.

## v0.1.0

The first release: the YAML 1.2 decoder with the core schema, built on goccy/go-yaml v1.19.2, with merge keys, an alias
limit and `WithLegacyNumbers` for YAML 1.1 integers. Measured against yaml-test-suite in docs/conformance.md.
