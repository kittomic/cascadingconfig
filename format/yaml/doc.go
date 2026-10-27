// Package yaml is cascadingconfig's YAML decoder, for YAML 1.2 with the core schema, built on
// github.com/goccy/go-yaml.
//
// Register it with cascadingconfig.WithDecoder(yaml.Decoder()); the presets already do. Unquoted values get their types
// from YAML 1.2's rules, not YAML 1.1's, so yes and no are strings; [WithLegacyNumbers] accepts YAML 1.1's integer
// forms too.
//
// [Conformance] measures it against yaml-test-suite.
//
// [Conformance]: https://github.com/kittomic/cascadingconfig/blob/main/docs/conformance.md
package yaml
