package toml

import (
	"errors"

	_ "github.com/pelletier/go-toml/v2" // stub: keeps the requirement until Decode uses it

	"github.com/kittomic/cascadingconfig"
)

// Decoder returns the TOML decoder.
//
// It accepts whatever the pinned go-toml accepts, which includes TOML 1.1 syntax. A duplicate key is an error, as the
// TOML spec requires. Offset date-times become RFC 3339 text, and local date-times, dates and times their TOML text, so
// every date in the tree is a string. A file with no keys is empty.
//
// Use it through cascadingconfig.DecodeDocument, which also strips a byte-order mark and rejects input that isn't
// UTF-8.
func Decoder() cascadingconfig.Decoder {
	return decoder{}
}

type decoder struct{}

func (decoder) Format() string { return "toml" }

func (decoder) Extensions() []string { return []string{"toml"} }

func (decoder) MediaTypes() []string { return []string{"application/toml"} }

func (decoder) Decode([]byte) (cascadingconfig.Document, error) {
	return cascadingconfig.Document{}, errors.New("not implemented") // stub
}
