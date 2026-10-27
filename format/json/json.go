package json

import (
	"github.com/kittomic/cascadingconfig"
	"github.com/kittomic/cascadingconfig/internal/json"
)

// Decoder returns the JSON decoder.
//
// It accepts strict RFC 8259 JSON: no comments and no trailing commas. A duplicate key keeps its last value and adds a
// cascadingconfig.WarnDuplicateKey warning. Integers are int64 when the literal has no fraction or exponent and fits;
// one out of range is an error. A whitespace-only or null document is empty, and the top level must otherwise be an
// object.
//
// Use it through cascadingconfig.DecodeDocument, which also strips a byte-order mark and rejects input that isn't
// UTF-8. The core layouts register a decoder that wraps the same parser.
func Decoder() cascadingconfig.Decoder {
	return decoder{}
}

type decoder struct{}

func (decoder) Format() string { return "json" }

func (decoder) Extensions() []string { return []string{"json"} }

func (decoder) MediaTypes() []string { return []string{"application/json"} }

func (decoder) Decode(data []byte) (cascadingconfig.Document, error) {
	res, err := json.Parse(data)
	if err != nil {
		return cascadingconfig.Document{}, err
	}
	doc := cascadingconfig.Document{Tree: res.Tree, Empty: res.Empty}
	for _, d := range res.Duplicates {
		doc.Warnings = append(doc.Warnings, cascadingconfig.Warning{
			Code: cascadingconfig.WarnDuplicateKey, Key: cascadingconfig.Path(d.Key), Message: d.Message,
		})
	}
	return doc, nil
}
