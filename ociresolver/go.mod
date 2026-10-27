module github.com/kittomic/cascadingconfig/ociresolver

go 1.26.0

// Until the first release the core can't be fetched, because the repository is private, so this module builds against
// the repository's copy (DL-162). M11 replaces this with the released version.
replace github.com/kittomic/cascadingconfig => ../

require (
	github.com/kittomic/cascadingconfig v0.0.0-00010101000000-000000000000
	github.com/opencontainers/image-spec v1.1.1
	oras.land/oras-go/v2 v2.6.4
)

require (
	github.com/go-viper/mapstructure/v2 v2.5.0 // indirect
	github.com/opencontainers/go-digest v1.0.0 // indirect
	golang.org/x/sync v0.24.0 // indirect
)
