package cascadingconfig

import (
	"slices"
	"strings"
)

// WithDecoder registers a decoder. A decoder for a format that already has one replaces it. Two formats claiming the
// same extension are an *OptionError.
func WithDecoder(d Decoder) Option {
	return optionFunc(func(c *config) {
		if d == nil {
			c.fail("WithDecoder", "a nil decoder")
			return
		}
		for _, other := range c.decoders {
			if other.Format() == d.Format() {
				continue
			}
			for _, ext := range d.Extensions() {
				if slices.Contains(other.Extensions(), ext) {
					c.fail("WithDecoder", "the extension %q is claimed by both %q and %q", ext, other.Format(), d.Format())
					return
				}
			}
		}
		if i := slices.IndexFunc(c.decoders, func(o Decoder) bool { return o.Format() == d.Format() }); i >= 0 {
			c.decoders[i] = d
			return
		}
		c.decoders = append(c.decoders, d)
	})
}

// WithFormats sets the format order, highest precedence first, and enables only the formats listed. It takes the same
// syntax as {NAME}_CONFIG_FORMATS: an extension stands for its format, and leading dots and surrounding spaces are
// allowed. Every format needs a registered decoder. It replaces WithDefaultFormats.
func WithFormats(formats ...string) Option {
	formats = slices.Clone(formats)
	return optionFunc(func(c *config) {
		if len(formats) == 0 {
			c.fail("WithFormats", "no formats are listed")
			return
		}
		c.formats = formats
	})
}

// WithDefaultFormats sets a layout's default format order. Unlike WithFormats, it may name formats with no registered
// decoder, which are simply not searched.
func WithDefaultFormats(formats ...string) Option {
	formats = slices.Clone(formats)
	return optionFunc(func(c *config) {
		if len(formats) == 0 {
			c.fail("WithDefaultFormats", "no formats are listed")
			return
		}
		c.defaultFormats = formats
	})
}

// WithFormatConflict sets what happens when one slot matches more than one file. The default is ConflictMerge.
func WithFormatConflict(policy FormatConflict) Option {
	return optionFunc(func(c *config) {
		if policy < ConflictMerge || policy > ConflictFail {
			c.fail("WithFormatConflict", "unknown policy %d", int(policy))
			return
		}
		c.conflict = policy
	})
}

// finishFormats works out the format order, now that every decoder is registered.
func (c *config) finishFormats() {
	switch {
	case c.formats != nil:
		c.order = c.resolveFormats("WithFormats", c.formats, true)
	case c.defaultFormats != nil:
		for _, f := range c.resolveFormats("WithDefaultFormats", c.defaultFormats, false) {
			if c.decoder(f) != nil {
				c.order = append(c.order, f)
			}
		}
	default:
		for _, d := range c.decoders {
			c.order = append(c.order, d.Format())
		}
	}
}

// resolveFormats turns a format list as written into format names. With strict, every entry needs a registered
// decoder; otherwise an entry no decoder knows is kept as written. A format listed twice is an error either way.
func (c *config) resolveFormats(op string, formats []string, strict bool) []string {
	var out []string
	for _, entry := range formats {
		name := strings.TrimPrefix(strings.TrimSpace(entry), ".")
		if name == "" {
			c.fail(op, "an empty format in %q", formats)
			continue
		}
		format, ok := c.formatFor(name)
		if !ok {
			if strict {
				c.fail(op, "%q isn't the format or extension of a registered decoder", name)
				continue
			}
			format = name
		}
		if slices.Contains(out, format) {
			c.fail(op, "the format %q is listed twice", format)
			continue
		}
		out = append(out, format)
	}
	return out
}

// formatFor returns the registered format that name is, or whose extension it is.
func (c *config) formatFor(name string) (string, bool) {
	for _, d := range c.decoders {
		if d.Format() == name || slices.Contains(d.Extensions(), name) {
			return d.Format(), true
		}
	}
	return "", false
}
