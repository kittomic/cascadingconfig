// Package ociresolver fetches cascadingconfig's oci includes from container registries, such as
// oci::ghcr.io/acme/myapp-config:1.4.0, built on oras.land/oras-go/v2.
//
// Register it with cascadingconfig.WithIncludeResolver(ociresolver.New()); mise.Preset already does. A reference with an
// @sha256: digest is pinned. No credentials are sent unless [WithCredentials] supplies them, and ~/.docker/config.json
// is never read.
//
// See [Includes].
//
// [Includes]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/includes.md
package ociresolver
