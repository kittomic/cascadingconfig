package yaml

import (
	"errors"

	_ "github.com/goccy/go-yaml" // stub: keeps the requirement until Decode uses it

	"github.com/kittomic/cascadingconfig"
)

// Decoder returns the YAML decoder, for YAML 1.2 with the core schema.
//
// goccy/go-yaml parses the file, and the core schema's rules give unquoted values their types: yes, no, on and off are
// strings, 017 is 17 and 1e3 is a float. Only the core schema's tags are accepted. A key becomes a string by printing
// its resolved value, so 0x1F: becomes "31", and keys that clash are an error. Merge keys (<<) are supported, earlier
// entries winning. At most 10,000 values may come from aliases and merge keys. A file holds one document.
//
// Use it through cascadingconfig.DecodeDocument, which also strips a byte-order mark and rejects input that isn't
// UTF-8.
func Decoder(opts ...Option) cascadingconfig.Decoder {
	_ = opts // stub
	return decoder{}
}

// Option configures the YAML decoder.
type Option interface {
	apply(c *config)
}

type config struct{}

type optionFunc func(c *config)

func (f optionFunc) apply(c *config) { f(c) }

// WithLegacyNumbers accepts the YAML 1.1 integer forms that goccy and yaml.v3 accept: 017 as octal, 0b11 as binary, and
// _ between digits.
func WithLegacyNumbers() Option {
	return optionFunc(func(*config) {}) // stub
}

// WithMaxAliasValues sets how many values aliases and merge keys may create in one document, 10,000 by default. A value
// below 1 makes every Decode fail, because Decoder has no error to return.
func WithMaxAliasValues(n int) Option {
	_ = n // stub
	return optionFunc(func(*config) {})
}

type decoder struct{}

func (decoder) Format() string { return "yaml" }

func (decoder) Extensions() []string { return []string{"yaml", "yml"} }

func (decoder) MediaTypes() []string {
	return []string{"application/yaml", "application/x-yaml", "text/yaml", "text/x-yaml"}
}

func (decoder) Decode([]byte) (cascadingconfig.Document, error) {
	return cascadingconfig.Document{}, errors.New("not implemented") // stub
}
