module github.com/kittomic/cascadingconfig/internal/apicheck

go 1.26.0

require (
	github.com/kittomic/cascadingconfig v0.1.0
	github.com/kittomic/cascadingconfig/format/toml v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/format/yaml v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/ociresolver v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/presets v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/watch/fsnotify v0.0.0-00010101000000-000000000000
)

require (
	github.com/fsnotify/fsnotify v1.10.1 // indirect
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/goccy/go-yaml v1.19.2 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	github.com/opencontainers/image-spec v1.1.1 // indirect
	github.com/pelletier/go-toml/v2 v2.4.3 // indirect
	golang.org/x/sync v0.24.0 // indirect
	golang.org/x/sys v0.49.0 // indirect
	oras.land/oras-go/v2 v2.6.4 // indirect
)

// This module is test-only and never released, so it tracks the modules in this repository, also with GOWORK=off.
replace (
	github.com/kittomic/cascadingconfig => ../..
	github.com/kittomic/cascadingconfig/format/toml => ../../format/toml
	github.com/kittomic/cascadingconfig/format/yaml => ../../format/yaml
	github.com/kittomic/cascadingconfig/ociresolver => ../../ociresolver
	github.com/kittomic/cascadingconfig/presets => ../../presets
	github.com/kittomic/cascadingconfig/watch/fsnotify => ../../watch/fsnotify
)
