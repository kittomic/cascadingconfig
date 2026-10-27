# Guide

How to use `cascadingconfig`, task by task. Each page stands on its own; start with getting started if the library is
new to you. For the exact order in which files load, see [precedence](../precedence.md), and for working programs, the
[examples](../../examples/).

1. [Getting started](getting-started.md): loaders, presets, scopes and layers, and a first load.
2. [Where config comes from](sources.md): each preset's search places, environments, environment variables and flags,
   explicit files, and scopes of your own.
3. [Decoding](decoding.md): typed config, defaults, unknown keys and required config.
4. [Merging](merging.md): tables, arrays and null, rules for a key, and removing keys.
5. [Includes](includes.md): local and remote includes, policies, pinning and the cache.
6. [Security](security.md): `misehardened.Preset`, trusting project config, and permission checks.
7. [Watching](watching.md): reloading on changes, diffs, failed reloads, polling and remote includes.
8. [Provenance and logging](provenance-and-logging.md): the report, tracing a key, and what the loader logs.
9. [Testing your app](testing.md): a fake filesystem and host, a fake clock and notifier, and golden files.
10. [Errors](errors.md): codes, error types, and what a failed load returns.
