// Package misehardened provides [Preset]: cascadingconfig.MiseHardenedLayout with TOML, YAML and file watching. It's
// for config from places the app doesn't control, such as a cloned repository: there are no remote includes, local
// includes stay within their project or the user and system config directories, and files that others can write to are
// skipped.
//
// See [Security].
//
// [Security]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/security.md
package misehardened
