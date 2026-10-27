// Package json is cascadingconfig's JSON decoder, for strict RFC 8259 JSON. It uses only encoding/json.
//
// The core layouts already read JSON, so an app only needs this package to register the decoder in a layout of its own,
// or to call [Decoder] through cascadingconfig.DecodeDocument.
package json
