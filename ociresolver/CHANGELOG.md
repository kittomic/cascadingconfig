# Changelog: `github.com/kittomic/cascadingconfig/ociresolver`

All notable changes to this module. Entries are built from [Conventional Commits](https://www.conventionalcommits.org/)
whose scope is `ociresolver`, and each release is tagged `ociresolver/vX.Y.Z`.

## v0.1.0

The first release: a resolver for `oci::` includes from container registries, built on oras-go v2.6.4, reading
`oras push` files and image tar layers, with digest verification, size caps, opt-in credentials and opt-in plain HTTP.
