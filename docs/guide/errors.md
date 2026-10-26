# Errors

Every error the loader returns is typed, carries a code, and has a sentinel, so you can branch on the kind of problem
with `errors.Is`, read its details with `errors.As`, or just print it.

## Handling an error

<!-- example -->
```go
_, err := loader.Load(ctx)
switch {
case err == nil:
case errors.Is(err, cascadingconfig.ErrParse):
	var pe *cascadingconfig.ParseError
	if errors.As(err, &pe) {
		fmt.Printf("%s:%d: fix the syntax\n", pe.Path, pe.Line)
	}
case cascadingconfig.CodeOf(err) == cascadingconfig.CodeNotFound:
	fmt.Println("no config found")
default:
	fmt.Println(err)
}
```

- **`CodeOf(err)`** gives the code of the first loader error in the chain, following HTTP's numbers where they fit, so a
  CLI can map them to exit codes or a service to responses.
- **`Errors(err)`** splits a joined error into its parts. A load reports every problem a stage finds, joined, so one run
  shows every broken file, not just the first.
- **Every error embeds a `BaseError`** with `Op`, the operation, `Path`, the file, and `Scope`, and unwraps to its
  cause.
- **Messages never quote file contents,** since config can hold secrets. A parse error gives the line and column.

## The error types

| Type                      | Code      | When                                                                          |
| ------------------------- | --------- | ----------------------------------------------------------------------------- |
| `*OptionError`            | 400       | A loader option is invalid, such as an unknown scope or ambiguous merge rules |
| `*ParseError`             | 400       | A file isn't valid TOML, YAML or JSON, or a reserved key is malformed         |
| `*ReferenceError`         | 400       | An include reference is malformed or tries to escape its origin               |
| `*PolicyError`            | 403       | A policy blocked a file or include: an allow-list, pinning, trust or confinement |
| `*NotFoundError`          | 404       | An explicit file or include is missing, or `RequireConfig` wasn't met         |
| `*ConflictError`          | 409       | Two files in one place, under `ConflictFail`                                  |
| `*TooLargeError`          | 413       | A file or include is over its size limit, or there are too many includes      |
| `*UnsupportedFormatError` | 415       | No decoder for a file's format                                                |
| `*DecodeError`            | 422       | A key doesn't fit its field, or matches no field when unknown keys are errors |
| `*MergeError`             | 422       | A merge rule met the wrong types, or a merge function failed                  |
| `*TransformError`         | 422       | A transform hook failed                                                       |
| `*IOError`                | 500       | A named file couldn't be read, such as for its permissions                    |
| `*UnsupportedSchemeError` | 501       | An include's scheme has no resolver, such as a remote one in `misehardened.Preset` |
| `*FetchError`             | 502, 504  | A remote include couldn't be fetched; 504 for a timeout                       |
| `*CycleError`             | 508       | An include cycle, or includes nested too deep                                 |

- **Cancelled loads:** when the context ends, `errors.Is(err, context.Canceled)` or `context.DeadlineExceeded` holds.
- **Your callbacks' errors,** from a trust store, `ValuesFunc`, a custom scope or a matcher, come back as a code-500
  `*BaseError` whose `Op` names the callback, unwrapping to your error.
- **A searched file that can't be read** isn't an error: it's skipped with a warning, so one broken file in a directory
  you don't control can't stop your app. Files you name explicitly, and includes, are never skipped.

## A failed load's result

`loader.Load` returns a result even when it fails. Its report lists what was found, what loaded, and what broke, with a
broken file as `LayerFailed` and a reason, which is what a diagnostic command needs. Its `Tree()` is nil, so half-merged
config can't be used by mistake. `Load[T]` returns the zero value instead; use `loader.Load` when you want the report.
