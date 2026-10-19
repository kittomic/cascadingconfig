package cascadingconfig

// Path is a key path into a Tree, one segment per level. The empty Path is the root.
type Path []string

// PathSpec is accepted wherever the API takes a key path: either the dotted string form or a Path.
type PathSpec interface{ ~string | ~[]string }

// ParsePath parses the dotted string form of a path.
//
// The empty string is the root path. Empty segments, as in "a..b", ".a" or "a.", are an error. A segment can be
// double-quoted, as in TOML dotted keys, so plugins."foo.bar" is Path{"plugins", "foo.bar"}. Inside quotes, \" and \\
// escape a quote and a backslash.
func ParsePath(s string) (Path, error) {
	_ = s
	return nil, nil
}

// String returns the dotted form of p. It quotes any segment that is empty or contains '.', '"' or '\', so every Path
// round-trips through ParsePath.
func (p Path) String() string {
	_ = p
	return ""
}
