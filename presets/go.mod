module github.com/kittomic/cascadingconfig/presets

go 1.26.0

// Until the first release the other modules can't be fetched, because the repository is private, so this module builds
// against the repository's copies (DL-162, DL-249). M11 replaces these with the released versions.
replace (
	github.com/kittomic/cascadingconfig => ../
	github.com/kittomic/cascadingconfig/format/toml => ../format/toml
	github.com/kittomic/cascadingconfig/format/yaml => ../format/yaml
	github.com/kittomic/cascadingconfig/ociresolver => ../ociresolver
	github.com/kittomic/cascadingconfig/watch/fsnotify => ../watch/fsnotify
)

require (
	github.com/kittomic/cascadingconfig v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/format/toml v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/format/yaml v0.0.0-00010101000000-000000000000
	github.com/kittomic/cascadingconfig/ociresolver v0.0.0-00010101000000-000000000000
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
