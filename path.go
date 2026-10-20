package cascadingconfig

import (
	"errors"
	"fmt"
	"strings"
)

// Path is a key path into a Tree, one segment per level. The empty Path is the root.
type Path []string

// PathSpec is accepted wherever the API takes a key path: either the dotted string form or a Path.
type PathSpec interface{ ~string | ~[]string }

// ParsePath parses the dotted string form of a path.
//
// The empty string is the root path. Empty segments, as in "a..b", ".a" or "a.", are an error. A segment can be
// double-quoted, as in TOML dotted keys, so plugins."foo.bar" is Path{"plugins", "foo.bar"}. Inside quotes, \" and \\
// escape a quote and a backslash. A quote can't appear in an unquoted segment.
func ParsePath(s string) (Path, error) {
	p, err := parsePath(s)
	if err != nil {
		return nil, fmt.Errorf("cascadingconfig: %w", err)
	}
	return p, nil
}

// parsePath is ParsePath without the package prefix on errors, for callers that wrap them in their own errors.
func parsePath(s string) (Path, error) {
	if s == "" {
		return Path{}, nil
	}
	p := Path{}
	for i := 0; ; {
		var segment string
		var err error
		if s[i] == '"' {
			segment, i, err = parseQuotedSegment(s, i)
		} else {
			segment, i, err = parseBareSegment(s, i)
		}
		if err != nil {
			return nil, fmt.Errorf("invalid path %q: %w", s, err)
		}
		p = append(p, segment)

		if i == len(s) {
			return p, nil
		}
		if s[i] != '.' {
			return nil, fmt.Errorf("invalid path %q: unexpected %q after a quoted segment", s, s[i])
		}
		i++
		if i == len(s) {
			return nil, fmt.Errorf("invalid path %q: %w", s, errEmptySegment)
		}
	}
}

var errEmptySegment = errors.New("empty segment")

// parseBareSegment reads an unquoted segment starting at i, and returns it with the index just after it.
func parseBareSegment(s string, i int) (string, int, error) {
	start := i
	for i < len(s) && s[i] != '.' {
		if s[i] == '"' {
			return "", 0, errors.New("a quote inside an unquoted segment")
		}
		i++
	}
	if i == start {
		return "", 0, errEmptySegment
	}
	return s[start:i], i, nil
}

// parseQuotedSegment reads a double-quoted segment whose opening quote is at i, and returns it with the index just
// after the closing quote.
func parseQuotedSegment(s string, i int) (string, int, error) {
	var b strings.Builder
	for i++; i < len(s); i++ {
		switch c := s[i]; c {
		case '"':
			return b.String(), i + 1, nil
		case '\\':
			if i+1 == len(s) || (s[i+1] != '"' && s[i+1] != '\\') {
				return "", 0, errors.New(`only \" and \\ may be escaped in a quoted segment`)
			}
			i++
			b.WriteByte(s[i])
		default:
			b.WriteByte(c)
		}
	}
	return "", 0, errors.New("unterminated quoted segment")
}

// MarshalText returns the dotted form of p, so a Path encodes as a string.
func (p Path) MarshalText() ([]byte, error) {
	return nil, nil // stub
}

// UnmarshalText parses the dotted form, as ParsePath does.
func (p *Path) UnmarshalText(text []byte) error {
	_, _ = p, text // stub
	return nil
}

// String returns the dotted form of p. It quotes any segment that is empty or contains '.', '"' or '\', so every Path
// round-trips through ParsePath.
func (p Path) String() string {
	var b strings.Builder
	for i, segment := range p {
		if i > 0 {
			b.WriteByte('.')
		}
		if segment != "" && !strings.ContainsAny(segment, `."\`) {
			b.WriteString(segment)
			continue
		}
		b.WriteByte('"')
		for j := 0; j < len(segment); j++ {
			if segment[j] == '"' || segment[j] == '\\' {
				b.WriteByte('\\')
			}
			b.WriteByte(segment[j])
		}
		b.WriteByte('"')
	}
	return b.String()
}
