// Package httpresolver fetches cascadingconfig's http and https includes.
//
// Register it with cascadingconfig.WithIncludeResolver(httpresolver.New()); mise.Preset already does. The loader checks
// every reference against its policies before the resolver sees it, and a URL ending #sha256=<hex> is pinned: the
// content must have that hash. Credentials belong in the transport of the client given to [WithClient], never in a
// reference.
//
// See [Includes].
//
// [Includes]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/includes.md
package httpresolver
