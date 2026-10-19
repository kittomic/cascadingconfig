package cascadingconfig

// WithDecoder registers a decoder. A decoder for a format that already has one replaces it. Two formats claiming the
// same extension are an *OptionError.
func WithDecoder(d Decoder) Option {
	_ = d // stub
	return optionFunc(func(*config) {})
}

// WithFormats sets the format order, highest precedence first, and enables only the formats listed. It takes the same
// syntax as {NAME}_CONFIG_FORMATS: an extension stands for its format, and leading dots and surrounding spaces are
// allowed. Every format needs a registered decoder. It replaces WithDefaultFormats.
func WithFormats(formats ...string) Option {
	_ = formats // stub
	return optionFunc(func(*config) {})
}

// WithDefaultFormats sets a layout's default format order. Unlike WithFormats, it may name formats with no registered
// decoder, which are simply not searched.
func WithDefaultFormats(formats ...string) Option {
	_ = formats // stub
	return optionFunc(func(*config) {})
}

// WithFormatConflict sets what happens when one slot matches more than one file. The default is ConflictMerge.
func WithFormatConflict(policy FormatConflict) Option {
	_ = policy // stub
	return optionFunc(func(*config) {})
}
