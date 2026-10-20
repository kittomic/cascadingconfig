package cascadingconfig

import (
	"fmt"
	"net/url"
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
		if _, ok := m.(refMatcher); ok {
			c.fail("WithFileFormat", "MatchRef matches include references, so it only works in allow-lists")
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
	if f, ok := c.ruleFormat(slashPath); ok {
		return f, true
	}
	return c.extFormat(slashPath)
}

// ruleFormat returns the format of the last WithFileFormat rule matching s with a registered decoder.
func (c *config) ruleFormat(s string) (string, bool) {
	for i := len(c.fileFormats) - 1; i >= 0; i-- {
		r := c.fileFormats[i]
		if !r.m.matches(s, c.name) {
			continue
		}
		for _, f := range r.formats {
			if c.decoder(f) != nil {
				return f, true
			}
		}
	}
	return "", false
}

// extFormat returns the format of the registered decoder whose extensions include slashPath's.
func (c *config) extFormat(slashPath string) (string, bool) {
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

func (m extensionMatcher) describe() string { return "Extension(" + quoted(m.exts) + ")" }

// quoted writes each string Go-quoted, separated by commas.
func quoted(ss []string) string {
	out := make([]string, len(ss))
	for i, s := range ss {
		out[i] = fmt.Sprintf("%q", s)
	}
	return strings.Join(out, ", ")
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

func (m fileNameMatcher) describe() string { return fmt.Sprintf("FileName(%q)", m.glob) }

// wildcard turns a pattern whose only special character is * into a regexp in which * matches star.
func wildcard(pattern, star string) *regexp.Regexp {
	parts := strings.Split(pattern, "*")
	for i, p := range parts {
		parts[i] = regexp.QuoteMeta(p)
	}
	return regexp.MustCompile("^" + strings.Join(parts, star) + "$")
}

// Glob matches a file's whole slash path: * matches within one path segment, ** across segments, and ? one character
// other than /.
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
		case p[0] == '?':
			b.WriteString("[^/]")
			p = p[1:]
		default:
			i := strings.IndexAny(p, "*?")
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

func (m globMatcher) describe() string { return fmt.Sprintf("Glob(%q)", m.pattern) }

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

func (m regexpMatcher) describe() string { return fmt.Sprintf("Regexp(%q)", m.expr) }

// MatchFunc matches the paths f accepts. In an allow-list, f gets the normalised reference.
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

func (funcMatcher) describe() string { return "MatchFunc" }

// Prefix matches the paths, or normalised include references, that start with s. It's a plain string prefix, so end s
// with / to match a directory.
func Prefix(s string) Matcher {
	return prefixMatcher{s: s}
}

type prefixMatcher struct{ s string }

func (prefixMatcher) matcher() {}

func (m prefixMatcher) check(c *config) bool {
	if m.s == "" {
		c.fail("Prefix", "an empty prefix, which would match everything")
		return false
	}
	return true
}

func (m prefixMatcher) matches(slashPath, _ string) bool { return strings.HasPrefix(slashPath, m.s) }

func (m prefixMatcher) describe() string { return fmt.Sprintf("Prefix(%q)", m.s) }

// MatchRef matches the include references f accepts, with the whole IncludeRef, including the file that asked for it.
// It's for allow-lists only: in WithFileFormat it is an *OptionError (DL-176).
func MatchRef(f func(ref IncludeRef) bool) Matcher {
	return refMatcher{f: f}
}

type refMatcher struct{ f func(ref IncludeRef) bool }

func (refMatcher) matcher() {}

func (m refMatcher) check(c *config) bool {
	if m.f == nil {
		c.fail("MatchRef", "a nil function")
		return false
	}
	return true
}

// matches is never called: WithFileFormat rejects MatchRef, and allow-lists go through matchRef.
func (refMatcher) matches(string, string) bool { return false }

func (refMatcher) describe() string { return "MatchRef" }

// matchRef reports whether m accepts ref: a MatchRef gets the whole reference, Extension and FileName its file path,
// and any other matcher its normalised form (DL-202).
func matchRef(m Matcher, ref IncludeRef, name string) bool {
	switch m := m.(type) {
	case refMatcher:
		return m.f(ref)
	case extensionMatcher, fileNameMatcher:
		return m.matches(refFile(ref), name)
	}
	return m.matches(ref.Ref, name)
}

// refFile returns the file path in a reference: the path inside a repo or artifact, a URL's path, or a local path.
func refFile(ref IncludeRef) string {
	switch {
	case ref.Path != "":
		return ref.Path
	case ref.Scheme == "file":
		return ref.Location
	}
	if u, err := url.Parse(ref.Location); err == nil {
		return u.Path
	}
	return ref.Location
}

// mediaFormat returns the format of the registered decoder that handles the media type mt, without its parameters.
func (c *config) mediaFormat(mt string) (string, bool) {
	mt, _, _ = strings.Cut(mt, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	if mt == "" {
		return "", false
	}
	for _, d := range c.decoders {
		if slices.ContainsFunc(d.MediaTypes(), func(t string) bool { return strings.EqualFold(t, mt) }) {
			return d.Format(), true
		}
	}
	return "", false
}
