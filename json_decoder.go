package cascadingconfig

import "github.com/kittomic/cascadingconfig/internal/json"

// builtinJSONDecoder is the JSON decoder every core layout registers. format/json can't be imported here, because it
// imports this package, so both wrap the parser in internal/json and decode identically (DL-95).
var builtinJSONDecoder Decoder = builtinJSON{}

type builtinJSON struct{}

func (builtinJSON) Format() string { return "json" }

func (builtinJSON) Extensions() []string { return []string{"json"} }

func (builtinJSON) MediaTypes() []string { return []string{"application/json"} }

func (builtinJSON) Decode(data []byte) (Document, error) {
	res, err := json.Parse(data)
	if err != nil {
		return Document{}, err
	}
	doc := Document{Tree: res.Tree, Empty: res.Empty}
	for _, d := range res.Duplicates {
		doc.Warnings = append(doc.Warnings, Warning{Code: WarnDuplicateKey, Key: Path(d.Key), Message: d.Message})
	}
	return doc, nil
}
