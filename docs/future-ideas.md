# Future ideas

Ideas that are out of scope for now, kept so they aren't lost. Nothing here is decided. Each would need a design
decision before work starts. For things that are deliberately out of scope rather than postponed, see design.md's
[Non-goals](design.md#non-goals).

| Idea                          | Notes                                                                                                         |
| ----------------------------- | ------------------------------------------------------------------------------------------------------------- |
| **JSONC support**             | JSON with comments and trailing commas, as used by VS Code settings and `tsconfig.json`. It would be a separate `format/jsonc` module, so that strict JSON stays the default. The parser and its dependencies would need choosing. |
| **Combined-environment files** | Files such as `myapp.dev.linux.toml`, loaded only when every environment they name is active. They would have to be opt-in, because today `dev.linux` is an ordinary single environment name, as in mise (design.md [Non-goals](design.md#non-goals)). |
| **cosmiconfig's `$import`**   | Deferred, and possibly never supported (design.md DL-145). It resolves paths relative to the importing file, with no allow-list or confinement, so it would need fitting into the include security and trust model first. |
| **Parallel remote fetches**   | Remote includes are fetched one at a time, in walk order (design.md DL-199). Fetching a file's includes in parallel would speed up loads with many remotes, but must keep the result deterministic. |
| **Pruning the include cache** | The include cache never removes entries (design.md DL-206). Entries no reference has used for a while could be pruned by age or total size. |
| **Rename `wip` to `develop`** | The integration branch's name is cosmetic. It's deferred until there's a convenient moment ([implementation-plan.md](implementation-plan.md#decisions)). |
