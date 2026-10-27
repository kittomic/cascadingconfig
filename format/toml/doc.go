// Package toml is cascadingconfig's TOML decoder, built on github.com/pelletier/go-toml/v2.
//
// Register it with cascadingconfig.WithDecoder(toml.Decoder()); the presets already do. Dates and times become strings
// in the merged tree: an offset date-time is RFC 3339 text, which fills a time.Time, and local dates and times keep
// their TOML text.
//
// [Conformance] measures it against toml-test.
//
// [Conformance]: https://github.com/kittomic/cascadingconfig/blob/main/docs/conformance.md
package toml
