package json

import (
	"errors"

	"github.com/kittomic/cascadingconfig"
)

// errNotImplemented is returned while the decoder is declared but not yet implemented.
var errNotImplemented = errors.New("json: not implemented")

// Decoder returns the JSON decoder.
//
// It accepts strict RFC 8259 JSON: no comments and no trailing commas. A duplicate key keeps its last value and adds a
// cascadingconfig.WarnDuplicateKey warning. Integers are int64 when the literal has no fraction or exponent and fits;
// one out of range is an error. A whitespace-only or null document is empty, and the top level must otherwise be an
// object.
func Decoder() cascadingconfig.Decoder {
	return decoder{}
}

type decoder struct{}

func (decoder) Format() string { return "" }

func (decoder) Extensions() []string { return nil }

func (decoder) MediaTypes() []string { return nil }

func (decoder) Decode(data []byte) (cascadingconfig.Document, error) {
	_ = data
	return cascadingconfig.Document{}, errNotImplemented
}
