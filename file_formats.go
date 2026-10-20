package cascadingconfig

import (
	"path"
	"regexp"
	"slices"
	"strings"
)

// WithFileFormat decodes the files m accepts with the first listed format that has a registered decoder. Rules are
// checked before the decoders' extensions, the most recently registered first.
func WithFileFormat(m Matcher, formats ...string) Option {
	formats = slices.Clone(formats)
	return optionFunc(func(c *config) {
		switch {
		case m == nil:
			c.fail("WithFileFormat", "a nil matcher")
			return
		case len(formats) == 0:
			c.fail("WithFileFormat", "no formats are listed")
			return
		}
		if m.check(c) {
			c.fileFormats = append(c.fileFormats, fileFormatRule{m: m, formats: formats})
		}
	})
}

type fileFormatRule struct {
	m       Matcher
	formats []string
}

// fileFormat returns the format of the file at slashPath: the first rule that matches and names a registered format,
// the most recently registered first, or else the decoder whose extension the file has.
func (c *config) fileFormat(slashPath string) (string, bool) {
	for i := len(c.fileFormats) - 1; i >= 0; i-- {
		r := c.fileFormats[i]
		if !r.m.matches(slashPath, c.name) {
			continue
		}
		for _, f := range r.formats {
			if c.decoder(f) != nil {
				return f, true
			}
		}
	}
	ext := strings.TrimPrefix(path.Ext(slashPath), ".")
	if ext == "" {
		return "", false
	}
	for _, d := range c.decoders {
		if slices.Contains(d.Extensions(), ext) {
			return d.Format(), true
		}
	}
	return "", false
}

// ruleMatches reports whether a WithFileFormat rule with a registered format matches slashPath.
func (c *config) ruleMatches(slashPath string) bool {
	for _, r := range c.fileFormats {
		if r.m.matches(slashPath, c.name) && slices.ContainsFunc(r.formats, func(f string) bool { return c.decoder(f) != nil }) {
			return true
		}
	}
	return false
}

// Extension matches files with any of the extensions, given with or without the dot.
func Extension(exts ...string) Matcher {
	out := make([]string, len(exts))
	for i, e := range exts {
		out[i] = strings.TrimPrefix(e, ".")
	}
	return extensionMatcher{exts: out}
}

type extensionMatcher struct{ exts []string }

func (extensionMatcher) matcher() {}

func (m extensionMatcher) check(c *config) bool {
	if len(m.exts) == 0 || slices.Contains(m.exts, "") {
		c.fail("Extension", "an empty extension list or an empty extension")
		return false
	}
	return true
}

func (m extensionMatcher) matches(slashPath, _ string) bool {
	return slices.Contains(m.exts, strings.TrimPrefix(path.Ext(slashPath), "."))
}

// FileName matches a file's base name against a glob, in which * matches any run of characters. It may use {name}.
func FileName(glob string) Matcher {
	return fileNameMatcher{glob: glob}
}

type fileNameMatcher struct{ glob string }

func (fileNameMatcher) matcher() {}

func (m fileNameMatcher) check(c *config) bool {
	if m.glob == "" || strings.ContainsAny(m.glob, `/\`) {
		c.fail("FileName", "the pattern %q must be a non-empty file name, without separators", m.glob)
		return false
	}
	return true
}

func (m fileNameMatcher) matches(slashPath, name string) bool {
	return wildcard(expand(m.glob, name, "", ""), ".*").MatchString(path.Base(slashPath))
}

// wildcard turns a pattern whose only special character is * into a regexp in which * matches star.
func wildcard(pattern, star string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, star) + "$")
}

// Glob matches a file's whole slash path: * matches within one path segment, and ** across segments.
func Glob(pattern string) Matcher {
	var b strings.Builder
	b.WriteString("^")
	for p := pattern; p != ""; {
		switch {
		case strings.HasPrefix(p, "**/"):
			b.WriteString("(?:.*/)?")
			p = p[3:]
		case strings.HasPrefix(p, "**"):
			b.WriteString(".*")
			p = p[2:]
		case p[0] == '*':
			b.WriteString("[^/]*")
			p = p[1:]
		default:
			i := strings.IndexByte(p, '*')
			if i < 0 {
				i = len(p)
			}
			b.WriteString(regexp.QuoteMeta(p[:i]))
			p = p[i:]
		}
	}
	b.WriteString("$")
	return globMatcher{pattern: pattern, re: regexp.MustCompile(b.String())}
}

type globMatcher struct {
	pattern string
	re      *regexp.Regexp
}

func (globMatcher) matcher() {}

func (m globMatcher) check(c *config) bool {
	if m.pattern == "" {
		c.fail("Glob", "an empty pattern")
		return false
	}
	return true
}

func (m globMatcher) matches(slashPath, _ string) bool { return m.re.MatchString(slashPath) }

// Regexp matches a file's whole slash path against a regular expression, which must match all of it.
func Regexp(expr string) Matcher {
	re, err := regexp.Compile("^(?:" + expr + ")$")
	return regexpMatcher{expr: expr, re: re, err: err}
}

type regexpMatcher struct {
	expr string
	re   *regexp.Regexp
	err  error
}

func (regexpMatcher) matcher() {}

func (m regexpMatcher) check(c *config) bool {
	if m.err != nil {
		c.fail("Regexp", "%q doesn't compile: %w", m.expr, m.err)
		return false
	}
	return true
}

func (m regexpMatcher) matches(slashPath, _ string) bool { return m.re.MatchString(slashPath) }

// MatchFunc matches the paths f accepts.
func MatchFunc(f func(path string) bool) Matcher {
	return funcMatcher{f: f}
}

type funcMatcher struct{ f func(path string) bool }

func (funcMatcher) matcher() {}

func (m funcMatcher) check(c *config) bool {
	if m.f == nil {
		c.fail("MatchFunc", "a nil function")
		return false
	}
	return true
}

func (m funcMatcher) matches(slashPath, _ string) bool { return m.f(slashPath) }
