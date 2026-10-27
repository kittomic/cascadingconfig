# Merging

Layers merge in precedence order, lowest first, so a higher layer wins wherever two set the same key. This page covers
what "wins" means for tables, arrays and null, and how to change it for a key.

## The rule

- **Tables merge key by key,** at every depth. A project file that sets only `server.port` keeps the `server.host` a
  user file set.
- **Everything else is replaced whole,** arrays included. A project's `tags = ["b"]` replaces a user's `tags = ["a"]`.
- **The same rule applies everywhere:** across scopes, environments, local files, a place's files in two formats, and
  includes.

To concatenate arrays instead, wherever no rule for the key says otherwise:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithArrayMerge(cascadingconfig.Append))
```

`cosmiconfig.Preset` appends by default, as cosmiconfig does.

## Rules for a key

A rule says how one key merges. Paths are dot-separated, and `*` matches exactly one segment:

<!-- example -->
```go
preset := mise.Preset.Extend(
	cascadingconfig.WithMergeRule("tools.*", cascadingconfig.Replace), // a tool's entry is replaced whole
	cascadingconfig.WithMergeRule("plugins", cascadingconfig.Append),  // plugins accumulate across layers
)
```

- **Strategies:** `DeepMerge`, the default for tables; `Replace`, the default for everything else, with nothing below it
  inherited; `Append` and `Prepend`, for arrays.
- **The most specific rule wins:** an exact path beats one with wildcards. Two rules that could both match a key, with
  nothing to choose between them, such as `a.*` and `*.b`, are an error from `New`.
- **Keys with dots:** use the list form, `cascadingconfig.Path{"plugins", "foo.bar"}`.
- **Mismatched types:** a rule that meets the wrong types, such as `Append` on a table, is a `*MergeError` naming the
  key and both files. Without a rule, merging never fails.

For anything else, a function decides:

<!-- example -->
```go
preset := mise.Preset.Extend(cascadingconfig.WithMergeFunc("limits.max",
	func(_ cascadingconfig.Path, lower, higher any) (any, error) {
		return max(lower.(int64), higher.(int64)), nil // the largest any layer allows
	}))
```

The function gets copies of both values, and its result is attributed to the higher layer. An error it returns is a
`*MergeError` that wraps it.

Rules are part of your app's schema, so presets never set them: `mise.Preset` deep-merges everything, even where mise
itself replaces whole sections such as `[tools]`. Declare the rules your app needs.

## Removing keys

A layer can remove keys that lower layers set, with the reserved key `_unset`:

```toml
# myapp.local.toml: no proxy here, and the default TLS settings go
_unset = ["proxy", "server.tls"]

[server]
port = 8080
```

- **Paths** are relative to the table holding `_unset`, so `_unset = ["tls"]` inside `[server]` is the same as the
  example. A key with dots is a list of segments: `_unset = [["plugins", "foo.bar"]]`.
- **Order:** the keys are removed from the lower layers' result first, then the layer's own values merge on top, so a
  layer can unset a table and set parts of it again.
- **A higher layer** can set an unset key again.
- **The name:** `WithUnsetKey` renames the key, and `WithoutUnsetKey` turns it off. Environment variables and flags
  can't use it.

## Null

In YAML and JSON, `null` is a value like any other. A higher `null` replaces the lower value, whatever its type, and a
higher value replaces a `null`. Unlike `_unset`, the key stays in the merged tree, with a nil value, and `OriginOf`
shows the file that set it to null. TOML has no null.

When decoding into a struct, a null leaves the field as it was: zero in a fresh struct, which `Load` always decodes
into. So `null` is how config removes a value that `WithDefaults` set, as `_unset` is.

## Two formats in one place

When one place holds the same name in two formats, such as `myapp.toml` and `myapp.yaml`, both load, and merge in format
order: the higher-ranked format, TOML by default, wins where they disagree. `WithFormatConflict` changes that:
`ConflictFirst` loads only the higher-ranked file, with a warning, and `ConflictFail` makes it an error.
