module github.com/kittomic/cascadingconfig/internal/compat

go 1.26.0

require (
	github.com/kittomic/cascadingconfig v0.1.0
	github.com/kittomic/cascadingconfig/format/toml v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/format/yaml v0.0.0-00010101000000-000000000000
	github.com/knadh/koanf/v2 v2.4.1
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/knadh/koanf/maps v0.1.2 // indirect
	github.com/mitchellh/copystructure v1.2.0 // indirect
	github.com/mitchellh/reflectwalk v1.0.2 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
)

// This module is test-only and never released, so it tracks the modules in this repository, also with GOWORK=off.
replace github.com/kittomic/cascadingconfig => ../..

replace github.com/kittomic/cascadingconfig/format/toml => ../../format/toml

replace github.com/kittomic/cascadingconfig/format/yaml => ../../format/yaml
