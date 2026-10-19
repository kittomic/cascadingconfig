package cascadingconfig

// Tree is a decoded or merged configuration: a nested map with string keys.
//
// It is an alias, so a Tree is a map[string]any and passes straight to koanf, mapstructure and encoding/json.
//
// Every decoder produces only these canonical value types, so that values from different formats merge, compare and
// diff consistently:
//
//   - nil, for null (YAML and JSON only)
//   - bool
//   - int64, for literals with no fraction or exponent that fit in range
//   - float64
//   - string, including every date and time, in RFC 3339 form
//   - []any
//   - map[string]any
type Tree = map[string]any
