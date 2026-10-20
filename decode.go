package cascadingconfig

import (
	"context"

	"github.com/go-viper/mapstructure/v2"
)

// Load runs l.Load and decodes the result into a fresh T, which can be any type mapstructure decodes into. On failure
// it returns the zero T and the error; use Loader.Load when the report is needed.
func Load[T any](ctx context.Context, l *Loader) (T, error) {
	var v T
	_, _ = ctx, l // stub
	return v, nil
}

// Decode decodes the merged tree into v, which must be a non-nil pointer, as koanf's Unmarshal does. Each key that
// fails is its own *DecodeError, joined. On a failed result, it returns the load's error again.
func (r *Result) Decode(v any) error {
	_ = v // stub
	return nil
}

// DecodeAt decodes the subtree at path into v, as koanf's Unmarshal(path, …) does. A path that isn't in the tree is not
// an error, and v is left untouched. A path string that doesn't parse, or a v that isn't a non-nil pointer, is a
// *BaseError with CodeBadRequest. On a failed result, it returns the load's error again.
func DecodeAt[P PathSpec](r *Result, path P, v any) error {
	_, _, _ = r, path, v // stub
	return nil
}

// DecodeHook returns the decode hooks koanf uses, composed in koanf's order: day-suffix durations ("7d"), other
// duration strings, and encoding.TextUnmarshaler, which also parses RFC 3339 strings into time.Time. An app that sets
// its own hooks with WithDecoderConfig can compose these into them.
func DecodeHook() mapstructure.DecodeHookFunc {
	return nil // stub
}

// WithTag sets the struct tag that decoding and WithDefaults read, instead of koanf. An empty tag is an *OptionError.
func WithTag(tag string) Option {
	_ = tag // stub
	return optionFunc(func(*config) {})
}

// WithDecoderConfig replaces the decoder configuration, hooks included, as koanf's UnmarshalConf.DecoderConfig does.
// The config is copied. Result is set on every decode, and an empty TagName falls back to the tag. A nil config, or
// one with Metadata, is an *OptionError: unknown keys are in the report and the error instead.
func WithDecoderConfig(dc *mapstructure.DecoderConfig) Option {
	_ = dc // stub
	return optionFunc(func(*config) {})
}

// WithFlatPaths decodes from flattened keys, such as "server.port", as koanf's FlatPaths does.
func WithFlatPaths() Option {
	return optionFunc(func(*config) {}) // stub
}

// UnknownKeys says what happens to keys in the tree that no field consumes.
type UnknownKeys int

// Unknown-key modes.
const (
	// UnknownKeysWarn logs each unknown key and records it in the report. It is the default.
	UnknownKeysWarn UnknownKeys = iota
	// UnknownKeysError makes each unknown key a *DecodeError with Unknown set.
	UnknownKeysError
	// UnknownKeysIgnore neither logs nor records them.
	UnknownKeysIgnore
)

// WithUnknownKeys sets what happens to unknown keys. An unknown mode is an *OptionError.
func WithUnknownKeys(u UnknownKeys) Option {
	_ = u // stub
	return optionFunc(func(*config) {})
}
