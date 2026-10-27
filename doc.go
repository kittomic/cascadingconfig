// Package cascadingconfig finds, merges and decodes layered configuration, the way mise and cosmiconfig do. System,
// user and project config, environment-specific files, environment variables and flags are merged in a defined order
// into one typed struct, and every value can be traced back to the file or variable that set it.
//
// Most apps start from a preset in the presets module, such as mise.Preset, which adds TOML, YAML, remote includes and
// file watching to this module's layouts. This module alone reads JSON and has one third-party dependency.
//
// # Loading
//
// A [Loader] is built once, from the app's name and a [Preset], and loads as often as needed. [Load] decodes the merged
// config into a fresh value of the app's type; [Loader.Load] returns the [Result], with the merged [Tree] and a report
// of what was found:
//
//	loader, err := cascadingconfig.NewWithPreset("myapp", mise.Preset)
//	if err != nil {
//		return err
//	}
//	cfg, err := cascadingconfig.Load[Config](ctx, loader)
//
// The name fills {name} in every place the preset searches. This module's layouts are [MiseLayout],
// [MiseHardenedLayout] and [CosmiconfigLayout]; [Preset.Extend] adds options to one without changing it, and
// [NewPreset] starts a layout of the app's own. A loader is safe for concurrent use.
//
// See [Getting started].
//
// # Scopes and layers
//
// A preset searches in scopes, from lowest precedence to highest, and every file a scope finds becomes a layer. Layers
// merge in order, so a later layer wins wherever two set the same key. MiseLayout's scopes are system, user, project
// (the working directory and the directories above it) and override.
//
// [WithScope] adds a scope of any kind: [EnvVars] for environment variables, [Values] for flags or other values from
// code, [FixedDir], [Walk], [ExplicitFile] and [EmbeddedFS] for files, and [Custom] for an app's own discovery.
// [WithEnvs] selects environments such as dev or prod, whose files rank above the shared ones. [WithConfigFiles] loads
// files the user names, as a --config flag does.
//
// See [Where config comes from] and [Precedence], which gives the exact order.
//
// # Decoding
//
// Keys are matched to struct fields by their koanf tags, as [WithTag] can change. [WithDefaults] supplies the lowest
// layer from a struct, [WithUnknownKeys] decides what a key with no field does, and [RequireConfig] fails a load that
// found no config. [DecodeAt] decodes one subtree, and [WithDecodeHook] converts values into the app's own types.
// Durations accept days, and [encoding.TextUnmarshaler] types decode from strings.
//
// See [Decoding].
//
// # Merging
//
// Tables merge key by key, and arrays and scalars are replaced. [WithArrayMerge] changes how every array merges, and
// [WithMergeRule] or [WithMergeFunc] how one key does. A null replaces the value below it, so its field decodes as
// zero, and a file's _unset key removes keys set by the layers below it.
//
// See [Merging].
//
// # Includes and policies
//
// With [WithIncludeKey], a file can include others, local or remote. Remote references are fetched by an
// [IncludeResolver] registered with [WithIncludeResolver]: the httpresolver and gitresolver packages in this module,
// and the ociresolver module. Policies, for every scope with [WithDefaultPolicy] or for one with [WithScopePolicy],
// decide which references are allowed ([Allow], [RequirePinned], [Confine]), which files are trusted ([RequireTrust])
// and which file permissions pass ([FilePermissions]).
//
// See [Includes] and [Security].
//
// # Watching
//
// [Watch] reloads when a file changes, and calls a handler with the old and new config and a [KeyDiff] for each changed
// key. A failed reload keeps the last good config. File events come from a [Notifier], such as the watch/fsnotify
// module's, set with [WithNotifier]; without one, files are polled.
//
// See [Watching].
//
// # Provenance and logging
//
// [Result.Describe] reports every scope, layer, environment and warning of a load, as text or JSON. [OriginOf] says
// which layer set a key, and what it overrode. [WithLogger] sends the loader's records to a [log/slog] logger.
//
// See [Provenance and logging].
//
// # Errors
//
// A failed load returns every problem it found, joined: [Errors] splits them, and [CodeOf] gives each one's code, an
// HTTP status. Each kind of problem has its own type, such as [*ParseError] or [*PolicyError]. A failed load still
// returns a non-nil [*Result], whose report shows what broke.
//
// See [Handling errors].
//
// # Testing
//
// The cctest package gives a loader a world of its own: an in-memory filesystem, a fake host, a clock and a notifier,
// so an app's config tests don't depend on the machine they run on. The examples below use it.
//
// See [Testing your app].
//
// [Getting started]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/getting-started.md
// [Where config comes from]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/sources.md
// [Precedence]: https://github.com/kittomic/cascadingconfig/blob/main/docs/precedence.md
// [Decoding]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/decoding.md
// [Merging]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/merging.md
// [Includes]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/includes.md
// [Security]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/security.md
// [Watching]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/watching.md
// [Provenance and logging]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/provenance-and-logging.md
// [Handling errors]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/errors.md
// [Testing your app]: https://github.com/kittomic/cascadingconfig/blob/main/docs/guide/testing.md
package cascadingconfig
