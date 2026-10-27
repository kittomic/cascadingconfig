// Package gitresolver fetches cascadingconfig's git includes, such as
// git::https://github.com/acme/config.git//myapp/base.toml?ref=v1.4.0, by running the git program, which must be
// installed.
//
// Register it with cascadingconfig.WithIncludeResolver(gitresolver.New()); mise.Preset already does. Git runs isolated
// from the user's and the system's config, reads only from the object database of a fresh bare repository, and uses
// only https and ssh unless [WithProtocols] says otherwise. A ref that's a full commit id is pinned.
//
// See [Includes].
//
// [Includes]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/includes.md
package gitresolver
